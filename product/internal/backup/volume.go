package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Volumes is the little of `app` a volume's backup needs.
type Volumes interface {
	// VolumeOf is one of an app's volumes, by the app's reference.
	VolumeOf(ctx context.Context, caller *user.User, ref string, id int64) (*Volume, error)
	// VolumeByID is one volume whatever app it is on, for the timer.
	VolumeByID(ctx context.Context, id int64) (*Volume, error)
	// WithAppStopped stops the app's container, runs fn, and starts it
	// again whatever fn returned, with no deploy running meanwhile.
	WithAppStopped(ctx context.Context, appID int64, fn func() error) error
}

// Volume is a volume as this module sees it.
type Volume struct {
	ID    int64
	AppID int64
	// App is the app's reference, "project/environment/app".
	App  string
	Path string
	// Dir is where its data is on this machine.
	Dir      string
	OnWorker bool
}

var (
	// ErrVolumeOnWorker refuses backing up or restoring a volume whose
	// data is on another machine: this daemon cannot read that disk.
	ErrVolumeOnWorker = errors.New("this volume's data is on another server, and backing those up is not available yet")

	// ErrEmptyArchive is a volume with nothing in it. An app that keeps
	// state has written something, so an empty copy is one that did not
	// happen — and restoring it would empty the volume.
	ErrEmptyArchive = errors.New("the volume is empty, so there is nothing to back up")

	// ErrNoVolumes is a server wired without apps, which only a test is.
	ErrNoVolumes = errors.New("volume backups are not available on this server")
)

// SetVolumes wires `app`. Called once, at startup, by `server`.
func (s *Service) SetVolumes(v Volumes) { s.volumes = v }

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

// ForVolume is one volume's backups, newest first.
func (s *Service) ForVolume(ctx context.Context, caller *user.User, ref string, id int64) ([]*Backup, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	return s.Repo().ForVolume(ctx, v.ID)
}

// TakeVolume starts one now: the app is stopped for the copy.
func (s *Service) TakeVolume(ctx context.Context, caller *user.User, ref string, id int64) (*Backup, error) {
	v, err := s.resolveVolume(ctx, caller, ref, id)
	if err != nil {
		return nil, err
	}
	schedule, err := s.volumeScheduleOrNothing(ctx, v.ID)
	if err != nil {
		return nil, err
	}
	storeID, bucket := int64(0), ""
	if schedule != nil {
		storeID, bucket = schedule.StoreID, schedule.Bucket
	}
	return s.startVolume(ctx, v, storeID, bucket, false)
}

func (s *Service) startVolume(ctx context.Context, v *Volume, storeID int64, bucket string, scheduled bool) (*Backup, error) {
	if v.OnWorker {
		return nil, ErrVolumeOnWorker
	}
	off := storeID != 0 && s.stores.LeavesThisMachine(ctx, storeID)
	row, err := s.Repo().Start(ctx, &Backup{
		Kind: KindVolume, VolumeID: v.ID, VolumePath: v.Path,
		DatastoreName: v.App, Engine: "volume",
		StoreID: storeID, Bucket: bucket, Off: off,
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

		size, err := s.dumpVolume(work, v, row)
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

// dumpVolume streams a tar.gz of the directory into the sink while the app
// is stopped. Nothing is staged: tar knows each file's size from the disk.
func (s *Service) dumpVolume(ctx context.Context, v *Volume, row *Backup) (int64, error) {
	sink, err := s.open(ctx, row)
	if err != nil {
		return 0, err
	}
	counted := &counter{w: sink}
	entries := 0
	copyErr := s.volumes.WithAppStopped(ctx, v.AppID, func() error {
		var err error
		entries, err = writeDirArchive(v.Dir, counted)
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

// writeDirArchive writes every entry under dir, keeping modes and owners —
// a queue's files belong to the user its image runs as, and a restore that
// handed them to root would leave it unable to start. Symlinks are kept as
// links, never followed. It answers how many entries it wrote.
func writeDirArchive(dir string, out io.Writer) (int, error) {
	gz := gzip.NewWriter(out)
	archive := tar.NewWriter(gz)
	entries := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() && !info.IsDir() {
			return nil // sockets and pipes are not data
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			header.Name += "/"
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		entries++
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(archive, f)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("archive the volume: %w", err)
	}
	if err := archive.Close(); err != nil {
		return 0, fmt.Errorf("close the archive: %w", err)
	}
	return entries, gz.Close()
}

// restoreVolume replaces a volume's directory with a backup of it.
//
// Extracted beside the directory first, and swapped in only once that
// worked: a restore that fails partway leaves the data as it was.
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
		return ErrVolumeOnWorker
	}
	r, err := s.read(ctx, row)
	if err != nil {
		return err
	}
	defer r.Close()

	return s.volumes.WithAppStopped(ctx, v.AppID, func() error {
		staged := v.Dir + ".restore"
		previous := v.Dir + ".previous"
		_ = os.RemoveAll(staged)
		if err := extractDirArchive(r, staged); err != nil {
			_ = os.RemoveAll(staged)
			return err
		}
		// The directory itself is not in the archive, so it keeps the
		// owner and mode the current one has.
		if info, err := os.Stat(v.Dir); err == nil {
			_ = os.Chmod(staged, info.Mode().Perm())
			if uid, gid, ok := owner(info); ok {
				_ = os.Lchown(staged, uid, gid)
			}
		}
		_ = os.RemoveAll(previous)
		if err := os.Rename(v.Dir, previous); err != nil && !os.IsNotExist(err) {
			_ = os.RemoveAll(staged)
			return fmt.Errorf("move the current data aside: %w", err)
		}
		if err := os.Rename(staged, v.Dir); err != nil {
			_ = os.Rename(previous, v.Dir)
			_ = os.RemoveAll(staged)
			return fmt.Errorf("put the restored data in place: %w", err)
		}
		if err := os.RemoveAll(previous); err != nil {
			log.Printf("backup: removing the replaced data of volume %d: %v", v.ID, err)
		}
		return nil
	})
}

// extractDirArchive unpacks what writeDirArchive wrote into dir. An entry
// that would land outside dir is refused rather than skipped: an archive
// with one is not one this instance wrote.
func extractDirArchive(in io.Reader, dir string) error {
	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("read the archive: %w", err)
	}
	defer gz.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	archive := tar.NewReader(gz)
	for {
		h, err := archive.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		name := filepath.FromSlash(strings.TrimSuffix(h.Name, "/"))
		if !filepath.IsLocal(name) {
			return fmt.Errorf("the archive has an entry outside the volume: %q", h.Name)
		}
		mode := os.FileMode(h.Mode) & os.ModePerm
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			_ = root.Chmod(name, mode)
		case tar.TypeReg:
			f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, archive)
			if err := f.Close(); copyErr == nil {
				copyErr = err
			}
			if copyErr != nil {
				return copyErr
			}
		case tar.TypeSymlink:
			if err := root.Symlink(h.Linkname, name); err != nil {
				return err
			}
		default:
			continue
		}
		// Owners only take when the daemon is root, which it is on a VPS.
		_ = root.Lchown(name, h.Uid, h.Gid)
	}
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
	if v.OnWorker {
		return nil, ErrVolumeOnWorker
	}
	if err := s.checkSchedule(ctx, store, &in); err != nil {
		return nil, err
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
