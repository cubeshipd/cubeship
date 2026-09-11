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
	"time"

	"cubeship/internal/user"
)

// Instance is the little of this daemon's own machinery a backup of the
// instance needs.
//
// An interface the way `Databases` and `Stores` are, and for the same
// reason: what it takes to reach Cubeship's own Postgres is a container
// and an Engine, and every use case here would otherwise be untestable
// without both.
type Instance interface {
	// OwnsDatabase reports whether this instance runs the Postgres it
	// stores everything in. False when it was pointed at somebody
	// else's server with CUBESHIP_DATABASE_URL — there is no container
	// here to dump through, and refusing says so.
	OwnsDatabase() bool
	// DumpDatabase streams a logical dump of it. The stderr it returns
	// is the only place Postgres explains why it refused.
	DumpDatabase(ctx context.Context, w io.Writer) (stderr string, err error)
	// Version is the Postgres major the instance is running, recorded
	// on the row so a dump can say what would read it back.
	Version() string
}

// ErrNoInstanceDatabase refuses a backup of an instance whose database
// is somebody else's server.
var ErrNoInstanceDatabase = errors.New(
	"this instance was pointed at a database it does not run, so there is no container here to dump — back that server up where it lives")

// InstanceFiles are the paths under the data directory that go into an
// instance backup, and the list is short on purpose.
//
// **What goes in is what cannot be worked out again.** The database is
// most of it. Beside it:
//
//   - `letsencrypt/acme.json` is every certificate this instance holds
//     and the private keys with them. Losing it is not fatal — Traefik
//     asks again — but asking again spends a weekly allowance shared
//     with everyone else under the same registered domain, and on a
//     default install that domain is `sslip.io`.
//   - `projects` is the pictures somebody chose. Small, and nothing
//     else has a copy.
//
// **What is deliberately left out is the data.** A datastore's
// directory and a managed store's objects are the two largest things on
// the box by orders of magnitude, and both already have a backup of
// their own that can go somewhere else — see the Backups tab on each.
// Folding them in here would make the one artifact that has to be small
// enough to take every night the one that is too big to take at all.
// The build cache is a cache, the setup token is spent, and
// `traefik-dynamic` is written from the rows on every start.
var InstanceFiles = []string{"letsencrypt/acme.json", "projects"}

// DatabaseEntry is what the dump is called inside the archive.
const DatabaseEntry = "cubeship.sql"

// TakeInstance backs up the instance itself.
//
// Same row, same destinations and same retention as a database's — see
// Kind for why this module is the one that can reach it at all.
func (s *Service) TakeInstance(ctx context.Context, caller *user.User) (*Backup, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	schedule, err := s.instanceScheduleOrNothing(ctx)
	if err != nil {
		return nil, err
	}
	storeID, bucket := int64(0), ""
	if schedule != nil {
		storeID, bucket = schedule.StoreID, schedule.Bucket
	}
	return s.startInstance(ctx, storeID, bucket, false)
}

// ListInstance is every backup of the instance, newest first.
func (s *Service) ListInstance(ctx context.Context, caller *user.User) ([]*Backup, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().ForInstance(ctx)
}

// startInstance writes the row and hands the work to a goroutine, the
// way a datastore's dump does and for the same reason: nobody is
// holding the connection, so where the outcome goes has to exist before
// there is one.
func (s *Service) startInstance(ctx context.Context, storeID int64, bucket string, scheduled bool) (*Backup, error) {
	if s.instance == nil || !s.instance.OwnsDatabase() {
		return nil, ErrNoInstanceDatabase
	}

	off := storeID != 0 && s.stores.LeavesThisMachine(ctx, storeID)
	row, err := s.Repo().Start(ctx, &Backup{
		Kind: KindInstance, DatastoreName: InstanceName,
		Engine: "postgres", Version: s.instance.Version(),
		StoreID: storeID, Bucket: bucket, Off: off,
		Key:       KeyFor(InstanceName, time.Now().UTC()),
		Scheduled: scheduled,
	})
	if err != nil {
		return nil, err
	}

	s.running.Add(1)
	go func() {
		defer s.running.Done()
		// A context of its own: whoever asked may already be gone.
		work, cancel := context.WithTimeout(context.WithoutCancel(ctx), Timeout)
		defer cancel()

		size, err := s.dumpInstance(work, row)
		failure := ""
		if err != nil {
			failure = err.Error()
		}
		if err := s.Repo().Finish(work, row.ID, size, failure); err != nil {
			log.Printf("backup: recording the instance backup: %v", err)
		}
		if failure == "" {
			s.pruneInstance(work)
		}
	}()
	return row, nil
}

// dumpInstance builds the archive and sends it.
//
// **This one is staged on disk, and a datastore's is not.** Every other
// dump here is a pipe from the engine straight into a multipart upload,
// because a database larger than the disk under it is the ordinary case
// on a small VPS. A tar entry has to declare its size before its bytes,
// so the same trick is not available — and it does not need to be: what
// is in here is rows about projects and apps rather than anybody's
// data, which is megabytes on an instance somebody has been running for
// a year. The file goes in the data directory and is removed whichever
// way this ends.
func (s *Service) dumpInstance(ctx context.Context, row *Backup) (int64, error) {
	staged, err := os.CreateTemp(s.dataDir, "instance-backup-*.tar.gz")
	if err != nil {
		return 0, fmt.Errorf("make room for the archive: %w", err)
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	if err := s.writeInstanceArchive(ctx, staged); err != nil {
		return 0, err
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("rewind the archive: %w", err)
	}

	sink, err := s.open(ctx, row)
	if err != nil {
		return 0, err
	}
	counted := &counter{w: sink}
	_, copyErr := io.Copy(counted, staged)
	closeErr := sink.Close(copyErr == nil)
	if copyErr != nil {
		return 0, fmt.Errorf("send the archive: %w", copyErr)
	}
	if closeErr != nil {
		return 0, fmt.Errorf("finish the archive: %w", closeErr)
	}
	if counted.n == 0 {
		return 0, ErrEmptyDump
	}
	return counted.n, nil
}

// writeInstanceArchive is the database and the files, gzipped.
func (s *Service) writeInstanceArchive(ctx context.Context, out io.Writer) error {
	gz := gzip.NewWriter(out)
	archive := tar.NewWriter(gz)

	// The dump first, because it is the reason the archive exists: a
	// reader that stops early still has the thing worth having.
	dump, err := os.CreateTemp(s.dataDir, "instance-dump-*.sql")
	if err != nil {
		return fmt.Errorf("make room for the dump: %w", err)
	}
	defer os.Remove(dump.Name())
	defer dump.Close()

	stderr, err := s.instance.DumpDatabase(ctx, dump)
	if err != nil {
		return withStderr(err, stderr)
	}
	info, err := dump.Stat()
	if err != nil {
		return fmt.Errorf("measure the dump: %w", err)
	}
	if info.Size() == 0 {
		return ErrEmptyDump
	}
	if _, err := dump.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind the dump: %w", err)
	}
	if err := archive.WriteHeader(&tar.Header{
		Name: DatabaseEntry, Mode: 0o600, Size: info.Size(), ModTime: time.Now(),
	}); err != nil {
		return fmt.Errorf("write the dump's header: %w", err)
	}
	if _, err := io.Copy(archive, dump); err != nil {
		return fmt.Errorf("write the dump: %w", err)
	}

	for _, name := range InstanceFiles {
		if err := s.addPath(archive, name); err != nil {
			return err
		}
	}

	if err := archive.Close(); err != nil {
		return fmt.Errorf("close the archive: %w", err)
	}
	return gz.Close()
}

// addPath copies one file or directory out of the data directory.
//
// **Missing is not an error.** A fresh instance has no certificates and
// no project pictures, and an archive that refused to be made because
// one of them is absent would be a backup that starts working only once
// somebody happens to have used the feature.
func (s *Service) addPath(archive *tar.Writer, name string) error {
	root := filepath.Join(s.dataDir, name)
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		// Only regular files. A symlink in here would be a path out of
		// the data directory written into somebody's archive.
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.dataDir, path)
		if err != nil {
			return err
		}
		if err := archive.WriteHeader(&tar.Header{
			Name: filepath.ToSlash(rel), Mode: 0o600,
			Size: info.Size(), ModTime: info.ModTime(),
		}); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(archive, f)
		return err
	})
}

func (s *Service) pruneInstance(ctx context.Context) {
	schedule, err := s.instanceScheduleOrNothing(ctx)
	if err != nil || schedule == nil {
		return
	}
	s.Prune(ctx, schedule)
}
