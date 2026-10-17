package app

import (
	"context"
	"cubeship/internal/metrics"
	"cubeship/internal/user"
	"fmt"
	"log"
)

func (s *Service) Delete(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	return s.DeleteApp(ctx, caller, ref, false)
}

// DeleteApp is Delete, saying what happens to the app's volumes' data.
// Kept by default: deleting an app is not deciding its data is worthless,
// and kept data is listed by VolumeOrphans until somebody removes it.
func (s *Service) DeleteApp(ctx context.Context, caller *user.User, ref Reference, deleteVolumeData bool) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := s.orch.Retire(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("stop the app's container: %w", err)
	}
	if err := s.Repo().Delete(ctx, a.ID); err != nil {
		return nil, err
	}
	if deleteVolumeData {
		s.dropVolumeData(ctx, a.Volumes)
	}
	if len(a.TCPPorts) > 0 {
		s.syncPorts(ctx)
	}
	return a, nil
}

// dropVolumeData deletes what it can reach of some volumes' data, after
// their rows are gone. Data on another machine is left, and said so in the
// log: the app is already deleted, and failing now would report a delete
// that happened as one that did not.
func (s *Service) dropVolumeData(ctx context.Context, volumes []Volume) {
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		log.Printf("delete volume data: %v", err)
		return
	}
	for _, v := range volumes {
		if v.NodeID != here {
			log.Printf("delete volume data: volume %d is on %s; its directory is left there", v.ID, v.NodeSlug)
			continue
		}
		if err := removeVolumeData(s.dataDir, v.ID); err != nil {
			log.Printf("delete volume data: volume %d: %v", v.ID, err)
		}
	}
}

// WithAppStopped stops an app's containers on this machine, runs fn, and
// starts them again whatever fn returned. No deploy runs meanwhile.
func (s *Service) WithAppStopped(ctx context.Context, appID int64, fn func() error) error {
	return s.orch.Paused(ctx, appID, fn)
}

// DeleteAppsInProject and DeleteAppsInEnvironment are
// project.AppTeardown: what deleting a project or an environment calls
// to take the apps under it out of service first.
//
// They take no caller. Authorization happened above — nobody reaches
// these without having been allowed to delete the thing that contains
// them — and checking the caller again here would ask a question already answered.
func (s *Service) DeleteAppsInProject(ctx context.Context, projectID int64) error {
	apps, err := s.Repo().ListForProject(ctx, projectID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

func (s *Service) DeleteAppsInEnvironment(ctx context.Context, environmentID int64) error {
	apps, err := s.Repo().ListForEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

// deleteAll retires and removes apps one at a time, stopping at the
// first failure.
//
// Stopping is deliberate. Retire already refuses to return success while
// a container it could not remove is still running, and carrying on past
// that would delete the rest of the rows and leave whoever asked with a
// container nothing on the instance names any more. What has been
// deleted stays deleted, and the delete above is refused — so a retry
// resumes rather than starting over.
func (s *Service) deleteAll(ctx context.Context, apps []*App) error {
	published := false
	defer func() {
		if published {
			s.syncPorts(ctx)
		}
	}()
	for _, a := range apps {
		published = published || len(a.TCPPorts) > 0
		if err := s.orch.Retire(ctx, a.ID); err != nil {
			return fmt.Errorf("stop app %q's container: %w", a.Name, err)
		}
		if err := s.Repo().Delete(ctx, a.ID); err != nil {
			return err
		}
		// Its history goes with it. Ids come from a sequence and are
		// reused across tables, so leaving these behind would give some
		// later app a chart of a stranger's.
		if err := s.metrics.Forget(ctx, metrics.KindApp, a.ID); err != nil {
			return err
		}
	}
	return nil
}
