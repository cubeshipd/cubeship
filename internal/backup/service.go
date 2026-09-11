package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cubeship/internal/datastore"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// manageRole is what taking, restoring or deleting a backup needs.
//
// An admin's, the same as the database itself: a dump is every row in
// it, so somebody who may read one may read everything the database
// holds — and a restore replaces all of it. `objectstore` draws the
// same line around a bucket's contents for the same reason.
const manageRole = user.RoleAdmin

// Timeout bounds one dump or one restore. It is nobody's request
// timeout — a backup is detached the moment it is asked for — it only
// stops a wedged command from holding a row in `taking` for ever.
const Timeout = 2 * time.Hour

// Databases is the little of `datastore` this module needs: which
// database, and a way to run a command inside its container.
//
// An interface rather than the service itself so the use cases here can
// be tested without a container anywhere — the same bargain
// `objectstore.Connector` makes.
type Databases interface {
	ByID(ctx context.Context, id int64) (*datastore.Datastore, error)
	BySlug(ctx context.Context, slug string) (*datastore.Datastore, error)
	// All is every database there is, for the coverage report — which
	// is about the ones with no backups, so it cannot be built from
	// the backups.
	All(ctx context.Context) ([]*datastore.Datastore, error)
	// Exec runs a command in the database's container, with stdin and
	// stdout streamed rather than collected. It reports what the
	// command wrote to stderr, which is the only place an engine
	// explains itself.
	Exec(ctx context.Context, d *datastore.Datastore, cmd, env []string, in io.Reader, out io.Writer) (stderr string, err error)
}

// Stores is the little of `objectstore` this module needs.
//
// Names and ids both, because they answer different questions: a
// schedule is written with the name somebody picked, and the row that
// records where a five-month-old dump went holds a key — a slug
// somebody could reuse is not one.
type Stores interface {
	ClientForID(ctx context.Context, id int64) (*objectstore.Store, objectstore.Client, error)
	IDForName(ctx context.Context, name string) (int64, error)
	NameForID(ctx context.Context, id int64) string
	// LeavesThisMachine separates a store somewhere else from the
	// MinIO this instance runs on its own disk. Asked once, when the
	// dump starts, and written on the row — see Backup.OffMachine.
	LeavesThisMachine(ctx context.Context, id int64) bool
}

type Service struct {
	db      *database.DB
	dbs     Databases
	stores  Stores
	dataDir string

	// running tracks dumps that outlive the request that asked for
	// one. Tests wait on it; the daemon does not.
	running sync.WaitGroup
}

func NewService(db *database.DB, dbs Databases, stores Stores, dataDir string) *Service {
	return &Service{db: db, dbs: dbs, stores: stores, dataDir: dataDir}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Wait blocks until every detached dump has finished. For tests, and
// for a daemon shutting down with one in flight.
func (s *Service) Wait() { s.running.Wait() }

// List is every backup this instance holds, newest first — including
// the ones whose database has been deleted, which is the only place
// those still appear.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Backup, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Coverage answers "is this instance protected", which is the one
// question a list of backups cannot.
//
// **It is built from the databases, not from the backups.** A database
// that has never been dumped is the row somebody most needs to see and
// it appears in no list of dumps — so the screen that listed every
// backup on the instance was, on an instance with two unprotected
// databases, showing nothing at all about either.
//
// Three reads and a join in memory rather than one query: the databases
// are another module's table and this one does not reach into it. It is
// a handful of rows on a box that runs a handful of databases, and the
// alternative is a query that would have to know a schema `datastore`
// owns.
func (s *Service) Coverage(ctx context.Context, caller *user.User) ([]*Coverage, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}

	databases, err := s.dbs.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	backups, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	schedules, err := s.Repo().Schedules(ctx)
	if err != nil {
		return nil, err
	}

	byDatastore := make(map[int64]*Schedule, len(schedules))
	for _, sc := range schedules {
		byDatastore[sc.DatastoreID] = sc
	}

	out := make([]*Coverage, 0, len(databases))
	index := make(map[int64]*Coverage, len(databases))
	for _, d := range databases {
		c := &Coverage{
			Database:  d.Slug,
			Engine:    string(d.Engine),
			Version:   d.Version,
			CanBackUp: d.Engine.CanBackUp(),
			Schedule:  byDatastore[d.ID],
		}
		out = append(out, c)
		index[d.ID] = c
	}

	// The listing is newest first, so the first of each kind seen is
	// the newest of it.
	for _, b := range backups {
		c := index[b.DatastoreID]
		if c == nil {
			continue // its database is gone; it is reported on its own
		}
		c.Count++
		if c.Last == nil {
			c.Last = b
		}
		if c.LastGood == nil && b.Status == StatusDone {
			c.LastGood = b
		}
	}
	return out, nil
}

// Orphans are the backups whose database has been deleted.
//
// They are kept on purpose — deleting a database is exactly the moment
// its backups matter — and they are the other half of what the instance
// screen shows, separately rather than mixed in: a row whose database
// is gone cannot be restored, and a table where some rows can and some
// cannot is one somebody reads wrong.
func (s *Service) Orphans(ctx context.Context, caller *user.User) ([]*Backup, error) {
	all, err := s.List(ctx, caller)
	if err != nil {
		return nil, err
	}
	out := []*Backup{}
	for _, b := range all {
		if b.DatastoreID == 0 {
			out = append(out, b)
		}
	}
	return out, nil
}

// ForDatabase is one database's backups.
func (s *Service) ForDatabase(ctx context.Context, caller *user.User, name string) ([]*Backup, error) {
	d, err := s.resolve(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	return s.Repo().ForDatastore(ctx, d.ID)
}

// Take starts one now and returns the row it will report into.
//
// Detached, like a deploy: a dump of anything worth backing up takes
// longer than a request should be held open, and a client that hangs up
// stops waiting rather than stops the backup. How it went lives in the
// row, which is why the row is written first.
func (s *Service) Take(ctx context.Context, caller *user.User, name string) (*Backup, error) {
	d, err := s.resolve(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	schedule, err := s.scheduleOrNothing(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	storeID, bucket := int64(0), ""
	if schedule != nil {
		storeID, bucket = schedule.StoreID, schedule.Bucket
	}
	return s.start(ctx, d, storeID, bucket, false)
}

// start writes the row and hands the work to a goroutine.
func (s *Service) start(ctx context.Context, d *datastore.Datastore, storeID int64, bucket string, scheduled bool) (*Backup, error) {
	if !d.Engine.CanBackUp() {
		return nil, fmt.Errorf("%w: %s", ErrNoDump, d.Engine)
	}

	// Asked here rather than on read, because the store can be deleted
	// and the row has to go on saying where this dump stands.
	off := storeID != 0 && s.stores.LeavesThisMachine(ctx, storeID)

	row, err := s.Repo().Start(ctx, &Backup{
		DatastoreID: d.ID, DatastoreName: d.Slug,
		Engine: string(d.Engine), Version: d.Version,
		StoreID: storeID, Bucket: bucket, Off: off,
		Key:       KeyFor(d.Slug, time.Now().UTC()),
		Scheduled: scheduled,
	})
	if err != nil {
		return nil, err
	}

	s.running.Add(1)
	go func() {
		defer s.running.Done()
		// A context of its own: whoever asked may already be gone, and
		// that must not stop a dump halfway and leave an object nobody
		// can restore from.
		ctx, cancel := context.WithTimeout(context.Background(), Timeout)
		defer cancel()
		s.run(ctx, d, row)
	}()
	return row, nil
}

// run takes the dump, reports what happened, and prunes.
//
// **Pruning is here rather than beside the call that started this**,
// because "keep 7" has to count the backup being taken — and it is not
// one of the seven until it has finished. Doing it on the way out is
// also the only place that knows whether it succeeded: a failed dump
// must not push a good one out of the window.
func (s *Service) run(ctx context.Context, d *datastore.Datastore, row *Backup) {
	size, err := s.dump(ctx, d, row)
	failure := ""
	if err != nil {
		failure = err.Error()
		log.Printf("backup: %s: %v", d.Slug, err)
	}
	if err := s.Repo().Finish(ctx, row.ID, size, failure); err != nil {
		log.Printf("backup: recording %s: %v", d.Slug, err)
		return
	}
	if failure != "" {
		return
	}
	schedule, err := s.scheduleOrNothing(ctx, d.ID)
	if err != nil || schedule == nil {
		return
	}
	s.Prune(ctx, schedule)
}

// dump streams the database into wherever this backup is going.
//
// **Nothing is held in memory and nothing lands on disk on the way.**
// The engine writes to a pipe, the far end reads it as the body of an
// upload, and the two run at once — so the largest database this
// instance can back up is not bounded by what it has left of either.
func (s *Service) dump(ctx context.Context, d *datastore.Datastore, row *Backup) (int64, error) {
	cmd, env, ok := d.Dump()
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrNoDump, d.Engine)
	}

	sink, err := s.open(ctx, row)
	if err != nil {
		return 0, err
	}

	// counted sits between the two so the size is measured as the bytes
	// go past, rather than by asking the far end afterwards — a local
	// file could be stat'd and a bucket could not, and one answer beats
	// two.
	counted := &counter{w: sink}
	stderr, execErr := s.dbs.Exec(ctx, d, cmd, env, nil, counted)

	// Closed before the error is judged: an upload is not complete
	// until it is, and a failure to finish it is a failure of the
	// backup however well the dump itself went.
	closeErr := sink.Close(execErr == nil)

	switch {
	case execErr != nil:
		return 0, withStderr(execErr, stderr)
	case closeErr != nil:
		return 0, fmt.Errorf("store the dump: %w", closeErr)
	case counted.n == 0:
		// An empty dump is never right — every engine writes a header
		// even for an empty database — and it is the shape a silent
		// failure takes. Better a backup that says it failed than one
		// that restores to nothing.
		return 0, errors.New("the dump was empty, which no engine produces even for an empty database")
	}
	return counted.n, nil
}

// withStderr puts what the engine said in front of what Go said. The
// second is "exit status 1" and the first is the reason.
func withStderr(err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return err
	}
	if len(stderr) > 2000 {
		stderr = stderr[:2000] + "…"
	}
	return fmt.Errorf("%s (%w)", stderr, err)
}

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
	dir := filepath.Join(s.dataDir, "backups", row.DatastoreName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("make room for the dump: %w", err)
	}
	path := filepath.Join(dir, filepath.Base(row.Key))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the dump: %w", err)
	}
	return &localSink{f: f, path: path}, nil
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
// bucket listing can sort and recognise. The timestamp is UTC and
// second-resolution: two backups of one database in the same second is
// somebody pressing a button twice, and the second one failing on the
// name is better than it overwriting the first.
func KeyFor(datastore string, at time.Time) string {
	return fmt.Sprintf("cubeship/%s/%s.dump", datastore, at.Format("2006-01-02T150405Z"))
}

// Restore loads a backup back into the database it came from.
//
// **It replaces what is there and cannot be undone**, which is why
// every surface in front of it asks for the database's own name first.
// What it does not do is stop the database: an app writing during a
// restore produces a state that is neither the backup nor what was
// there, and the screen says so rather than this pretending otherwise.
func (s *Service) Restore(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
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
	if err := user.Require(caller, manageRole); err != nil {
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
		f, err := os.Open(filepath.Join(s.dataDir, "backups", row.DatastoreName, filepath.Base(row.Key)))
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

// Delete removes a backup and the object behind it.
//
// **The row goes even when the object cannot be removed**, and the
// order is why: the object first, then the row. A row left behind for a
// file that is gone is a restore button that fails; a file left behind
// with no row is bytes in a bucket, which somebody can see and remove
// where they are. Of the two, the second is the one that does not lie.
func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
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
		path := filepath.Join(s.dataDir, "backups", row.DatastoreName, filepath.Base(row.Key))
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

func (s *Service) resolve(ctx context.Context, caller *user.User, name string) (*datastore.Datastore, error) {
	if err := user.Require(caller, manageRole); err != nil {
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
