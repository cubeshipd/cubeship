package app

import (
	"context"
	"cubeship/internal/user"
)

// Volumes is an app's volumes.
func (s *Service) Volumes(ctx context.Context, caller *user.User, ref Reference) ([]Volume, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, err
	}
	return a.Volumes, nil
}

// AddVolume gives an app a directory that outlives its container, on the
// machine the app runs on. It is mounted from the app's next deploy.
func (s *Service) AddVolume(ctx context.Context, caller *user.User, ref Reference, containerPath string) (*Volume, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	clean, err := CleanVolumePath(containerPath)
	if err != nil {
		return nil, err
	}
	for _, v := range a.Volumes {
		if v.Path == clean {
			return nil, ErrVolumeExists
		}
	}
	if !canHoldVolume(&a.App) {
		return nil, ErrVolumeNeedsOneCopy
	}
	nodeID := a.Replicas[0].NodeID
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	if nodeID == here && s.dataDir == "" {
		return nil, ErrNoDataDir
	}
	v, err := s.Repo().AddVolume(ctx, a.ID, clean, nodeID)
	if err != nil {
		return nil, err
	}
	// A worker makes the directory when it first mounts it, with its own
	// data directory. This machine makes it now, with the record that
	// says what it was should it ever be kept.
	if nodeID == here {
		if err := prepareVolume(s.dataDir, ReferenceOf(a).String(), v); err != nil {
			_ = s.Repo().RemoveVolume(ctx, a.ID, v.ID)
			return nil, err
		}
	}
	return v, nil
}

// RemoveVolume takes a volume off an app. Its data is kept unless
// deleteData, and the container running now keeps its mount until the
// next deploy.
func (s *Service) RemoveVolume(ctx context.Context, caller *user.User, ref Reference, volumeID int64, deleteData bool) error {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return err
	}
	var v *Volume
	for i := range a.Volumes {
		if a.Volumes[i].ID == volumeID {
			v = &a.Volumes[i]
		}
	}
	if v == nil {
		return ErrVolumeNotFound
	}
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	if deleteData && v.NodeID != here {
		return ErrVolumeOnWorker
	}
	if err := s.Repo().RemoveVolume(ctx, a.ID, v.ID); err != nil {
		return err
	}
	if deleteData {
		return removeVolumeData(s.dataDir, v.ID)
	}
	return nil
}

// VolumeOrphans is the data kept after its volumes were removed, on this
// machine.
func (s *Service) VolumeOrphans(ctx context.Context, caller *user.User) ([]Orphan, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	known, err := s.Repo().VolumeIDs(ctx)
	if err != nil {
		return nil, err
	}
	return orphans(s.dataDir, known)
}

// DeleteVolumeOrphan deletes kept data for good. An admin's: nothing
// says whose it was but a record on disk.
func (s *Service) DeleteVolumeOrphan(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Allow(caller, user.ResApps, user.LevelManage, ""); err != nil {
		return err
	}
	known, err := s.Repo().VolumeIDs(ctx)
	if err != nil {
		return err
	}
	found, err := orphans(s.dataDir, known)
	if err != nil {
		return err
	}
	for _, o := range found {
		if o.ID == id {
			return removeVolumeData(s.dataDir, id)
		}
	}
	return ErrOrphanNotFound
}

// VolumeTarget is a volume as `backup` sees it: whose it is, where its data
// is, and whether that is this machine.
type VolumeTarget struct {
	Volume
	App      Reference
	AppID    int64
	Dir      string
	OnWorker bool
}

// VolumeByID is one volume for a backup. It takes no caller: `backup`
// requires its own role before asking, and the scheduler has no caller.
func (s *Service) VolumeByID(ctx context.Context, id int64) (*VolumeTarget, error) {
	v, err := s.Repo().VolumeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	a, err := s.Repo().ScopedByID(ctx, v.AppID)
	if err != nil {
		return nil, ErrVolumeNotFound
	}
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	return &VolumeTarget{
		Volume: *v, App: ReferenceOf(a), AppID: a.ID,
		Dir: VolumeDir(s.dataDir, v.ID), OnWorker: v.NodeID != here,
	}, nil
}

// AllVolumes is every volume on the instance. It takes no caller, for the
// reason VolumeByID does not.
func (s *Service) AllVolumes(ctx context.Context) ([]*VolumeTarget, error) {
	apps, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	out := []*VolumeTarget{}
	for _, a := range apps {
		for _, v := range a.Volumes {
			out = append(out, &VolumeTarget{
				Volume: v, App: ReferenceOf(a), AppID: a.ID,
				Dir: VolumeDir(s.dataDir, v.ID), OnWorker: v.NodeID != here,
			})
		}
	}
	return out, nil
}

// VolumeOf is one of an app's volumes, by the app's reference.
func (s *Service) VolumeOf(ctx context.Context, caller *user.User, ref Reference, id int64) (*VolumeTarget, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, err
	}
	for _, v := range a.Volumes {
		if v.ID == id {
			return s.VolumeByID(ctx, id)
		}
	}
	return nil, ErrVolumeNotFound
}

// ServerFor is a machine by name: its id, and whether it is the control
// plane. For `backup`, which restores a volume onto it before MoveVolume.
func (s *Service) ServerFor(ctx context.Context, slug string) (int64, bool, error) {
	return s.Repo().ServerBySlug(ctx, slug)
}

// MoveVolume puts an app with one volume on another server and records the
// volume there. Its data has to be there already — `backup` restores it
// first — and the copy on the old server is left where it is.
//
// It takes no caller: `backup` requires its own role before asking.
func (s *Service) MoveVolume(ctx context.Context, volumeID int64, server string) error {
	v, err := s.Repo().VolumeByID(ctx, volumeID)
	if err != nil {
		return err
	}
	a, err := s.Repo().ScopedByID(ctx, v.AppID)
	if err != nil {
		return ErrVolumeNotFound
	}
	if len(a.Volumes) != 1 {
		return ErrVolumeMoveOne
	}
	nodeID, _, err := s.Repo().ServerBySlug(ctx, server)
	if err != nil {
		return err
	}
	if err := s.checkPlacement(ctx, server, Source(a.Source)); err != nil {
		return err
	}
	if err := s.Repo().SetVolumeNode(ctx, v.ID, nodeID); err != nil {
		return err
	}
	return s.replace(ctx, a, Placement{Nodes: []string{server}, Replicas: 1}, Source(a.Source))
}
