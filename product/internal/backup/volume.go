package backup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dirarchive"
	"cubeship/internal/user"
)

// Volumes is the little of `app` a volume's backup needs.
type Volumes interface {
	// VolumeOf is one of an app's volumes, by the app's reference.
	VolumeOf(ctx context.Context, caller *user.User, ref string, id int64) (*Volume, error)
	// VolumeByID is one volume whatever app it is on, for the timer.
	VolumeByID(ctx context.Context, id int64) (*Volume, error)
	// AllVolumes is every volume on the instance, for the coverage report.
	AllVolumes(ctx context.Context) ([]*Volume, error)
	// WithAppStopped stops the app's container, runs fn, and starts it
	// again whatever fn returned, with no deploy running meanwhile.
	WithAppStopped(ctx context.Context, appID int64, fn func() error) error
}

// Machines is how a volume on another server is backed up: that server
// makes the archive and sends it to S3 itself, so the data never passes
// through this one.
type Machines interface {
	RunVolumeJob(ctx context.Context, nodeID int64, kind string, job node.VolumeJob) (int64, error)
}

// Volume is a volume as this module sees it.
type Volume struct {
	ID     int64
	AppID  int64
	NodeID int64
	// App is the app's reference, "project/environment/app".
	App  string
	Path string
	// Dir is where its data is on this machine, when it is on this one.
	Dir      string
	OnWorker bool
}

var (
	// ErrVolumeOnWorker is a volume on another server with no way wired
	// to reach it, which only a test is.
	ErrVolumeOnWorker = errors.New("this volume's data is on another server, and this one cannot reach it")

	// ErrVolumeNeedsOffsite refuses a volume backup that would stay on
	// the instance. A database is dumped locally to be loaded back or
	// looked at; a volume's copy is only worth taking somewhere that
	// survives the machine, and is what moves it to another one.
	ErrVolumeNeedsOffsite = errors.New("a volume is backed up to an S3 bucket linked from outside this instance — not this machine's disk, and not a store this instance runs")

	// ErrEmptyArchive is a volume with nothing in it.
	ErrEmptyArchive = dirarchive.ErrEmpty

	// ErrNoVolumes is a server wired without apps, which only a test is.
	ErrNoVolumes = errors.New("volume backups are not available on this server")
)

// SetVolumes wires `app`. Called once, at startup, by `server`.
func (s *Service) SetVolumes(v Volumes) { s.volumes = v }

// SetMachines wires `node`. Called once, at startup, by `server`.
func (s *Service) SetMachines(m Machines) { s.machines = m }

// VolumeKeyFor is what a volume's backup is called where it lands.
func VolumeKeyFor(app string, volumeID int64, at time.Time) string {
	return fmt.Sprintf("cubeship/volumes/%s/%d/%s.tar.gz",
		strings.ReplaceAll(app, "/", "-"), volumeID, at.Format("2006-01-02T150405.000Z"))
}

func (s *Service) resolveVolume(ctx context.Context, caller *user.User, ref string, id int64) (*Volume, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if s.volumes == nil {
		return nil, ErrNoVolumes
	}
	return s.volumes.VolumeOf(ctx, caller, ref, id)
}

// offsite reports whether a store is somewhere a volume may be backed up to.
func (s *Service) offsite(ctx context.Context, storeID int64) bool {
	return storeID != 0 && s.stores.LeavesThisMachine(ctx, storeID)
}

// ForVolume is one volume's backups, newest first.
func (s *Service) ForVolume(ctx context.Context, caller *user.User, ref string, id int64) ([]*Backup, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	return s.Repo().ForVolume(ctx, v.ID)
}

// TakeVolume starts one now: the app is stopped for the copy. store and
// bucket say where it goes; empty takes the volume's schedule's.
func (s *Service) TakeVolume(ctx context.Context, caller *user.User, ref string, id int64, store, bucket string) (*Backup, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	var storeID int64
	if store != "" {
		if bucket == "" {
			return nil, ErrNoBucket
		}
		if storeID, err = s.stores.IDForName(ctx, store); err != nil {
			return nil, err
		}
		if _, _, err := s.stores.ClientForID(ctx, storeID); err != nil {
			return nil, err
		}
	} else {
		schedule, err := s.volumeScheduleOrNothing(ctx, v.ID)
		if err != nil {
			return nil, err
		}
		if schedule != nil {
			storeID, bucket = schedule.StoreID, schedule.Bucket
		}
	}
	return s.startVolume(ctx, v, storeID, bucket, false)
}

func (s *Service) startVolume(ctx context.Context, v *Volume, storeID int64, bucket string, scheduled bool) (*Backup, error) {
	if !s.offsite(ctx, storeID) {
		return nil, ErrVolumeNeedsOffsite
	}
	if v.OnWorker && s.machines == nil {
		return nil, ErrVolumeOnWorker
	}
	row, err := s.Repo().Start(ctx, &Backup{
		Kind: KindVolume, VolumeID: v.ID, VolumePath: v.Path,
		DatastoreName: v.App, Engine: "volume",
		StoreID: storeID, Bucket: bucket, Off: true,
		Key:       VolumeKeyFor(v.App, v.ID, time.Now().UTC()),
		Scheduled: scheduled,
	})
	if err != nil {
		return nil, err
	}

	s.running.Add(1)
	go func() {
		defer s.running.Done()
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), Timeout)
		defer cancel()

		size, err := s.copyVolume(work, v, row)
		failure := ""
		if err != nil {
			failure = err.Error()
			log.Printf("backup: volume %s of %s: %v", v.Path, v.App, err)
		}
		if err := s.Repo().Finish(work, row.ID, size, failure); err != nil {
			log.Printf("backup: recording volume %s of %s: %v", v.Path, v.App, err)
			return
		}
		if failure != "" {
			return
		}
		if schedule, err := s.volumeScheduleOrNothing(work, v.ID); err == nil && schedule != nil {
			s.Prune(work, schedule)
		}
	}()
	return row, nil
}

// copyVolume takes the archive where the volume is: here, or on the
// server that holds it.
func (s *Service) copyVolume(ctx context.Context, v *Volume, row *Backup) (int64, error) {
	if !v.OnWorker {
		return s.dumpVolume(ctx, v, row)
	}
	job, err := s.volumeJob(ctx, v, row)
	if err != nil {
		return 0, err
	}
	return s.machines.RunVolumeJob(ctx, v.NodeID, node.CommandVolumeBackup, job)
}

// volumeJob is what a server needs to copy a volume to or from a backup's
// object: where it is, and the login.
func (s *Service) volumeJob(ctx context.Context, v *Volume, row *Backup) (node.VolumeJob, error) {
	store, _, err := s.stores.ClientForID(ctx, row.StoreID)
	if err != nil {
		return node.VolumeJob{}, err
	}
	return node.VolumeJob{ID: v.ID, App: v.App, S3: node.S3Object{
		Endpoint: store.EndpointHost(), Region: store.Region,
		Secure: store.Secure, PathStyle: store.PathStyle,
		AccessKey: store.AccessKey, SecretKey: store.SecretKey,
		Bucket: row.Bucket, Key: row.Key,
	}}, nil
}

// dumpVolume streams a tar.gz of a volume on this machine into the sink
// while the app is stopped. Nothing is staged: tar knows each file's size
// from the disk.
func (s *Service) dumpVolume(ctx context.Context, v *Volume, row *Backup) (int64, error) {
	sink, err := s.open(ctx, row)
	if err != nil {
		return 0, err
	}
	counted := &counter{w: sink}
	entries := 0
	copyErr := s.volumes.WithAppStopped(ctx, v.AppID, func() error {
		var err error
		entries, err = dirarchive.Write(v.Dir, counted)
		return err
	})
	if copyErr == nil && entries == 0 {
		copyErr = ErrEmptyArchive
	}
	closeErr := sink.Close(copyErr == nil)
	switch {
	case copyErr != nil:
		return 0, copyErr
	case closeErr != nil:
		return 0, fmt.Errorf("store the archive: %w", closeErr)
	}
	return counted.n, nil
}

// restoreVolume replaces a volume's data with a backup of it, on the
// machine the volume is on. It is unpacked beside the data first, so a
// restore that fails leaves the data as it was.
func (s *Service) restoreVolume(ctx context.Context, row *Backup) error {
	if row.VolumeID == 0 {
		return fmt.Errorf("%w: the volume it came from has been removed", ErrNotFound)
	}
	if s.volumes == nil {
		return ErrNoVolumes
	}
	v, err := s.volumes.VolumeByID(ctx, row.VolumeID)
	if err != nil {
		return ErrNotFound
	}
	if v.OnWorker {
		if s.machines == nil {
			return ErrVolumeOnWorker
		}
		if row.StoreID == 0 {
			return fmt.Errorf("%w: this backup is on the control plane's own disk, which the volume's server cannot read", ErrVolumeNeedsOffsite)
		}
		job, err := s.volumeJob(ctx, v, row)
		if err != nil {
			return err
		}
		_, err = s.machines.RunVolumeJob(ctx, v.NodeID, node.CommandVolumeRestore, job)
		return err
	}

	r, err := s.read(ctx, row)
	if err != nil {
		return err
	}
	defer r.Close()
	return s.volumes.WithAppStopped(ctx, v.AppID, func() error {
		return dirarchive.Replace(r, v.Dir)
	})
}

// --- schedules ---

// VolumeSchedule reads one volume's, or ErrNotFound — which is off.
func (s *Service) VolumeSchedule(ctx context.Context, caller *user.User, ref string, id int64) (*Schedule, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	schedule, err := s.volumeScheduleOrNothing(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	if schedule == nil {
		return nil, ErrNotFound
	}
	return schedule, nil
}

// SetVolumeSchedule turns one on, or moves it.
func (s *Service) SetVolumeSchedule(ctx context.Context, caller *user.User, ref string, id int64, store string, in Schedule) (*Schedule, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	if err := s.checkSchedule(ctx, store, &in); err != nil {
		return nil, err
	}
	if !s.offsite(ctx, in.StoreID) {
		return nil, ErrVolumeNeedsOffsite
	}
	in.VolumeID = v.ID
	return s.Repo().SetVolumeSchedule(ctx, &in)
}

// UnsetVolumeSchedule turns it off.
func (s *Service) UnsetVolumeSchedule(ctx context.Context, caller *user.User, ref string, id int64) error {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return err
	}
	return s.Repo().DeleteVolumeSchedule(ctx, v.ID)
}

func (s *Service) volumeScheduleOrNothing(ctx context.Context, volumeID int64) (*Schedule, error) {
	schedule, err := s.Repo().VolumeScheduleFor(ctx, volumeID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	return schedule, err
}

func (s *Service) runScheduledVolume(ctx context.Context, schedule *Schedule, at time.Time) {
	if s.volumes == nil {
		return
	}
	v, err := s.volumes.VolumeByID(ctx, schedule.VolumeID)
	if err != nil {
		log.Printf("backup: the volume for a schedule is gone: %v", err)
		return
	}
	if err := s.Repo().MarkVolumeRun(ctx, schedule.VolumeID, at); err != nil {
		log.Printf("backup: marking volume %s of %s: %v", v.Path, v.App, err)
		return
	}
	if _, err := s.startVolume(ctx, v, schedule.StoreID, schedule.Bucket, true); err != nil {
		log.Printf("backup: starting one for volume %s of %s: %v", v.Path, v.App, err)
	}
}
