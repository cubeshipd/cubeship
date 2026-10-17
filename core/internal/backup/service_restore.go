package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cubeship/internal/datastore"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// sink is where a dump is being written, and closing one is where an
// upload is finished or abandoned.
type sink interface {
	io.Writer
	// Close(keep) finishes the object when keep, and removes whatever
	// was written when not: a half-uploaded dump left behind is one
	// somebody restores from.
	Close(keep bool) error
}

func (s *Service) open(ctx context.Context, row *Backup) (sink, error) {
	if row.StoreID == 0 {
		return s.openLocal(row)
	}
	return s.openStore(ctx, row)
}

// openLocal writes beside the database's own data, which is not a
// backup and every screen that shows one says so. It is here because it
// works the minute an instance is installed, with nothing linked and
// nothing configured — and a dump on the same disk still survives
// somebody dropping a table.
func (s *Service) openLocal(row *Backup) (sink, error) {
	path := s.localPath(row)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("make room for the dump: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the dump: %w", err)
	}
	return &localSink{f: f, path: path}, nil
}

// localPath is where a backup on this machine's own disk is: under the
// database's name, and a volume's under the key's own directories.
func (s *Service) localPath(row *Backup) string {
	if row.Kind == KindVolume {
		return filepath.Join(s.dataDir, "backups", filepath.FromSlash(strings.TrimPrefix(row.Key, "cubeship/")))
	}
	return filepath.Join(s.dataDir, "backups", row.DatastoreName, filepath.Base(row.Key))
}

type localSink struct {
	f    *os.File
	path string
}

func (l *localSink) Write(p []byte) (int, error) { return l.f.Write(p) }

func (l *localSink) Close(keep bool) error {
	err := l.f.Close()
	if !keep {
		_ = os.Remove(l.path)
	}
	return err
}

// openStore uploads as the dump is produced.
//
// The size is not known in advance — an engine writes until it is done —
// so the client is told -1, which is what makes it a multipart upload.
// Holding the dump anywhere first to learn the number would be holding
// the whole database.
func (s *Service) openStore(ctx context.Context, row *Backup) (sink, error) {
	store, c, err := s.stores.ClientForID(ctx, row.StoreID)
	if err != nil {
		return nil, err
	}
	if row.Bucket == "" {
		return nil, ErrNoBucket
	}

	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- c.Put(ctx, row.Bucket, row.Key, pr, -1, "application/octet-stream")
	}()
	return &storeSink{
		pw: pw, done: done,
		remove: func() error { return c.Remove(ctx, row.Bucket, row.Key) },
		store:  store.Slug,
	}, nil
}

type storeSink struct {
	pw     *io.PipeWriter
	done   chan error
	remove func() error
	store  string
}

func (u *storeSink) Write(p []byte) (int, error) { return u.pw.Write(p) }

func (u *storeSink) Close(keep bool) error {
	if !keep {
		// Breaking the pipe is what stops the upload rather than
		// finishing it: an S3 client reading a reader that errors
		// abandons the multipart rather than committing a short object.
		_ = u.pw.CloseWithError(errors.New("the dump failed"))
		<-u.done
		_ = u.remove()
		return nil
	}
	if err := u.pw.Close(); err != nil {
		return err
	}
	return <-u.done
}

// counter counts what goes through it.
type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// KeyFor is what a backup is called where it lands.
//
// The database's name and the moment, in a path a person reading a
// bucket listing can sort and recognise. UTC and zero-padded, so
// sorting the listing lexically puts the oldest first.
//
// **To the millisecond, because a second is not fine enough to be an
// identity.** It was seconds, on the reasoning that two dumps of one
// database in the same second is somebody pressing a button twice and
// that the second one failing on the name beats it overwriting the
// first. Both halves of that were wrong. Only the local path failed —
// it opens with O_EXCL — while a bucket took the second object straight
// over the first, so the same act had two outcomes depending on where
// the dump was going. And the "failure" is recorded as a failed backup:
// indistinguishable on screen from the engine refusing, and never
// pruned, because a failed row is kept as evidence that a schedule is
// broken.
//
// There was a third answer neither half considered, which is to make
// the name unique. Nothing overwrites and nothing fails.
func KeyFor(datastore string, at time.Time) string {
	return fmt.Sprintf("cubeship/%s/%s.dump", datastore, at.Format("2006-01-02T150405.000Z"))
}

// Restore loads a backup back into the database it came from.
//
// **It replaces what is there and cannot be undone**, which is why
// every surface in front of it asks for the database's own name first.
// What it does not do is stop the database: an app writing during a
// restore produces a state that is neither the backup nor what was
// there, and the screen says so rather than this pretending otherwise.
// server is only for a volume's backup: naming another server than the
// volume's restores it there and moves the app with it.
func (s *Service) Restore(ctx context.Context, caller *user.User, id int64, server string) error {
	if err := user.Allow(caller, user.ResBackups, user.LevelManage, ""); err != nil {
		return err
	}
	row, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	switch {
	case row.Running():
		return ErrStillRunning
	case row.Status != StatusDone:
		return ErrNotDone
	case row.Kind == KindVolume:
		return s.restoreVolume(ctx, row, server)
	case row.DatastoreID == 0:
		// The database it came from is gone. Restoring means choosing
		// which one to load it into, and that is a decision this does
		// not make — see "restoring into a new datastore", which is
		// what answers it.
		return fmt.Errorf("%w: the database it came from has been deleted", ErrNotFound)
	}

	d, err := s.dbs.ByID(ctx, row.DatastoreID)
	if err != nil {
		return ErrNotFound
	}
	if string(d.Engine) != row.Engine || d.Version != row.Version {
		return fmt.Errorf("%w: the dump is %s %s and the database is %s %s",
			ErrEngineMismatch, row.Engine, row.Version, d.Engine, d.Version)
	}
	cmd, env, ok := d.Restore()
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoDump, d.Engine)
	}

	r, err := s.read(ctx, row)
	if err != nil {
		return err
	}
	defer r.Close()

	stderr, err := s.dbs.Exec(ctx, d, cmd, env, r, io.Discard)
	if err != nil {
		return withStderr(err, stderr)
	}
	return nil
}

// Download hands the dump itself over, which is the other half of a
// backup being worth having: one nobody can get at is one that is only
// useful to this instance.
func (s *Service) Download(ctx context.Context, caller *user.User, id int64) (io.ReadCloser, *Backup, error) {
	if err := user.AllowSecrets(caller, user.ResBackups, ""); err != nil {
		return nil, nil, err
	}
	row, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	if row.Running() {
		return nil, nil, ErrStillRunning
	}
	r, err := s.read(ctx, row)
	if err != nil {
		return nil, nil, err
	}
	return r, row, nil
}

func (s *Service) read(ctx context.Context, row *Backup) (io.ReadCloser, error) {
	if row.StoreID == 0 {
		f, err := os.Open(s.localPath(row))
		if err != nil {
			return nil, fmt.Errorf("open the dump: %w", err)
		}
		return f, nil
	}
	_, c, err := s.stores.ClientForID(ctx, row.StoreID)
	if err != nil {
		return nil, err
	}
	r, _, err := c.Get(ctx, row.Bucket, row.Key)
	if err != nil {
		return nil, fmt.Errorf("read the dump: %w", err)
	}
	return r, nil
}

// SettleInterrupted fails the backups a restart of this daemon left
// `taking`. The goroutine or the server doing the copy is gone, and a row
// left running can be neither restored nor deleted.
func SettleInterrupted(ctx context.Context, repo *Repository) error {
	return repo.FailTaking(ctx, "the daemon restarted while this backup was being taken")
}

// Delete removes a backup and the object behind it.
//
// **The row goes even when the object cannot be removed**, and the
// order is why: the object first, then the row. A row left behind for a
// file that is gone is a restore button that fails; a file left behind
// with no row is bytes in a bucket, which somebody can see and remove
// where they are. Of the two, the second is the one that does not lie.
func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Allow(caller, user.ResBackups, user.LevelManage, ""); err != nil {
		return err
	}
	row, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return ErrNotFound
	}
	if row.Running() {
		return ErrStillRunning
	}
	if err := s.remove(ctx, row); err != nil {
		return err
	}
	return s.Repo().Delete(ctx, row.ID)
}

func (s *Service) remove(ctx context.Context, row *Backup) error {
	if row.Status != StatusDone {
		// Nothing was stored, or what was is already gone: a failed
		// dump removes what it wrote before reporting.
		return nil
	}
	if row.StoreID == 0 {
		path := s.localPath(row)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove the dump: %w", err)
		}
		return nil
	}
	_, c, err := s.stores.ClientForID(ctx, row.StoreID)
	if err != nil {
		// The store itself is gone — deleted, or its row went with a
		// credential. There is nothing here that can reach the object,
		// and refusing would leave a row nobody can ever clear.
		return nil
	}
	if err := c.Remove(ctx, row.Bucket, row.Key); err != nil {
		return fmt.Errorf("remove the dump: %w", err)
	}
	return nil
}

func (s *Service) resolve(ctx context.Context, caller *user.User, name string, need user.Level) (*datastore.Datastore, error) {
	if err := user.Allow(caller, user.ResBackups, need, ""); err != nil {
		return nil, err
	}
	if err := user.Allow(caller, user.ResDatabases, user.LevelView, name); err != nil {
		if errors.Is(err, user.ErrHidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d, err := s.dbs.BySlug(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	return d, nil
}

func (s *Service) scheduleOrNothing(ctx context.Context, datastoreID int64) (*Schedule, error) {
	schedule, err := s.Repo().ScheduleFor(ctx, datastoreID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	return schedule, err
}

// instanceScheduleOrNothing is the same for the instance's own, whose
// absence is likewise not an error: no row is what off is.
func (s *Service) instanceScheduleOrNothing(ctx context.Context) (*Schedule, error) {
	schedule, err := s.Repo().InstanceSchedule(ctx)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	return schedule, err
}
