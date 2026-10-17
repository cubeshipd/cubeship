package backup

import (
	"context"
	"fmt"
	"io"
	"log"
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

	// instance is this daemon's own machinery, for backing the
	// instance itself up. Wired by `server`, and nil in a test that
	// does not need it — which is what ErrNoInstanceDatabase answers.
	instance Instance
	// volumes is `app`, for backing up its volumes. Wired by `server`.
	volumes Volumes
	// machines is `node`, for a volume on another server. Wired by `server`.
	machines Machines

	// running tracks dumps that outlive the request that asked for
	// one. Tests wait on it; the daemon does not.
	running sync.WaitGroup
}

func NewService(db *database.DB, dbs Databases, stores Stores, dataDir string) *Service {
	return &Service{db: db, dbs: dbs, stores: stores, dataDir: dataDir}
}

// SetInstance wires what it takes to back the instance itself up.
// Called once, at startup, by the only package that knows every module
// exists — the same seam every other pair here meets at.
func (s *Service) SetInstance(i Instance) { s.instance = i }

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Wait blocks until every detached dump has finished. For tests, and
// for a daemon shutting down with one in flight.
func (s *Service) Wait() { s.running.Wait() }

// List is every backup this instance holds, newest first — including
// the ones whose database has been deleted, which is the only place
// those still appear.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Backup, error) {
	if err := user.Allow(caller, user.ResBackups, user.LevelView, ""); err != nil {
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
	if err := user.Allow(caller, user.ResBackups, user.LevelView, ""); err != nil {
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
			Kind:      KindDatastore,
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
	// Volumes are rows too, keyed apart: a volume's id and a database's
	// share nothing.
	volumeIndex := map[int64]*Coverage{}
	if s.volumes != nil {
		volumes, err := s.volumes.AllVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("list volumes: %w", err)
		}
		volumeSchedules, err := s.Repo().VolumeSchedules(ctx)
		if err != nil {
			return nil, err
		}
		byVolume := make(map[int64]*Schedule, len(volumeSchedules))
		for _, sc := range volumeSchedules {
			byVolume[sc.VolumeID] = sc
		}
		for _, v := range volumes {
			c := &Coverage{
				Kind: KindVolume, VolumeID: v.ID, Volume: v.Path,
				Database: v.App, Engine: "volume",
				// A volume on another server is not backed up here yet,
				// and says so rather than reading as never backed up.
				CanBackUp: !v.OnWorker || s.machines != nil,
				Schedule:  byVolume[v.ID],
			}
			out = append(out, c)
			volumeIndex[v.ID] = c
		}
	}

	for _, b := range backups {
		var c *Coverage
		switch b.Kind {
		case KindDatastore:
			c = index[b.DatastoreID]
		case KindVolume:
			c = volumeIndex[b.VolumeID]
		}
		if c == nil {
			continue // what it was taken from is gone; it is reported on its own
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
		gone := (b.Kind == KindDatastore && b.DatastoreID == 0) ||
			(b.Kind == KindVolume && b.VolumeID == 0)
		if gone {
			out = append(out, b)
		}
	}
	return out, nil
}

// ForDatabase is one database's backups.
func (s *Service) ForDatabase(ctx context.Context, caller *user.User, name string) ([]*Backup, error) {
	d, err := s.resolve(ctx, caller, name, user.LevelView)
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
	d, err := s.resolve(ctx, caller, name, user.LevelManage)
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
		return 0, ErrEmptyDump
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
