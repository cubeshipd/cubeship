// Package backup takes a copy of a database this instance runs, puts it
// somewhere that is not this machine, and puts it back.
//
// **Its own module, above `datastore` and `objectstore`**, the way
// `certificates` sits above `app` and `settings`. It could have lived
// inside `datastore` — a backup is a fact about a database — and the
// thing that decides it is a question about later: the most valuable
// database on this box is Cubeship's own Postgres, which holds every
// user, project and app, and is not a datastore at all. A module that
// could never reach it is the wrong module.
//
// What stays in `datastore` is the difference between engines, where it
// already lives: `spec.dump` and `spec.restore` beside `env` and `cmd`.
// This module decides when, where and for how long; that one knows
// Postgres is `pg_dump` and Redis is a file.
//
// **A logical dump, and only that.** It is the one kind that works the
// same across five engines, survives a major version changing under it,
// and comes out as a single stream that can be sent somewhere else —
// which is the only property that makes any of this a backup. Copying
// the data directory is faster to restore and is pinned to the exact
// version it came from, and taken while the engine runs it is simply
// corrupt. Continuous archiving — WAL, binlog — is a different product:
// an archive, a base backup underneath it, retention interlocked with
// both, and a restore nobody rehearses.
package backup

import (
	"errors"
	"time"
)

// Status is where one backup got to.
const (
	// StatusRunning is a dump in flight. The row is written before the
	// work starts, for the reason a deployment row is: nobody is
	// holding the connection, so where the outcome goes has to exist
	// before there is one.
	StatusRunning = "running"
	StatusDone    = "succeeded"
	StatusFailed  = "failed"
)

// Backup is one copy of one database, and what it says about itself
// outlives the database it came from.
//
// The name, the engine and the version are written down rather than
// joined. Deleting a datastore is exactly the moment its backups
// matter, so they are kept — and a dump that could not say which engine
// and which major version produced it is one nobody can safely load
// anywhere.
type Backup struct {
	ID int64
	// DatastoreID is zero once the database is gone. The backup is not.
	DatastoreID   int64
	DatastoreName string
	Engine        string
	Version       string

	// StoreID is zero for a backup on this machine's own disk, which is
	// not a backup and every screen that shows one says so.
	StoreID int64
	Bucket  string
	Key     string
	Size    int64

	Status string
	Error  string
	// Scheduled says the timer asked for this one rather than a person.
	Scheduled  bool
	StartedAt  time.Time
	FinishedAt *time.Time
}

// Running reports a dump still in flight, which is the one state a
// backup cannot be restored from or deleted in.
func (b *Backup) Running() bool { return b.Status == StatusRunning }

// OffMachine reports whether this copy is somewhere other than the disk
// it was taken from.
//
// The distinction is the point of the feature. A dump beside the
// database it came from survives somebody dropping a table and nothing
// else — not the disk, not the machine, not the provider.
func (b *Backup) OffMachine() bool { return b.StoreID != 0 }

// Schedule is when a database is backed up without anybody asking, and
// **the row existing is what "scheduled" means**. There is no separate
// flag that could say off while a time sat beside it.
type Schedule struct {
	DatastoreID int64
	// At is a time of day, "03:00", in Timezone.
	//
	// A time of day rather than an interval, for the reason
	// `update.Scheduler` takes one: what is being chosen is when the
	// database may be busy and slow, and "every 24 hours from whenever
	// you turned it on" is not something anybody can plan around. The
	// timezone is there because 03:00 on a server's clock is not the
	// middle of anybody's night.
	At       string
	Timezone string
	// Keep is how many to hold on to, newest first. **Zero keeps every
	// one**, which is a decision somebody can make rather than a gap —
	// and the screen says what it costs, because nothing here prunes a
	// bucket on its own.
	Keep int
	// StoreID and Bucket are where they go. Zero is this machine's own
	// disk: it works the minute the instance is installed, and it is
	// not a backup.
	StoreID   int64
	Bucket    string
	LastRunAt *time.Time
}

var (
	// ErrNotFound is a backup, or a schedule, this instance does not
	// hold.
	ErrNotFound = errors.New("no such backup")

	// ErrStillRunning refuses deleting or restoring a dump that has not
	// finished. Something is still writing to it, and to the object it
	// names.
	ErrStillRunning = errors.New("this backup is still being taken")

	// ErrNotDone refuses restoring from a backup that failed. What is
	// at the other end is a partial dump, and loading one is a database
	// left half replaced.
	ErrNotDone = errors.New("this backup did not finish, so there is nothing complete to restore")

	// ErrEngineMismatch refuses loading a dump into an engine that did
	// not produce it. `pg_dump` output is not MySQL's, and a major
	// version apart is a restore that fails partway with the database
	// already half replaced.
	ErrEngineMismatch = errors.New("this backup came from a different engine or major version")

	// ErrNoDump reports an engine this release cannot back up. Nothing
	// reaches it today — every engine has a dump or a file — and it is
	// here so that adding one without deciding this is a failure rather
	// than a button that does nothing.
	ErrNoDump = errors.New("this release does not know how to back that engine up")

	// ErrBadTime refuses a time of day that is not one.
	ErrBadTime = errors.New(`a time is "HH:MM" on a 24-hour clock`)

	// ErrUnknownTimezone refuses a zone this machine cannot resolve.
	// The database is embedded in the binary — see cmd/cubeshipd — so
	// this is a name nobody has, rather than an image missing tzdata.
	ErrUnknownTimezone = errors.New("that is not a timezone this machine knows")

	// ErrBadKeep refuses a negative count. Zero is "every one" and is
	// deliberate; below that is a typo.
	ErrBadKeep = errors.New("keep is how many to hold on to, and cannot be negative")

	// ErrNoBucket reports a store named with no bucket to put anything
	// in. A store holds many and a backup goes in one.
	ErrNoBucket = errors.New("name the bucket in that store to put backups in")
)

// MaxKeep is a typo limit rather than a resource one: somebody asking
// to keep ten thousand nightly dumps meant a hundred, and the rule
// would obediently work towards it for years.
const MaxKeep = 3650

// ParseTimeOfDay reads "HH:MM" and answers the hour and minute.
//
// Its own function because two places need the same answer — the
// service refusing one somebody typed, and the scheduler working out
// when next — and a second parser is a second opinion about what
// "24:00" means.
func ParseTimeOfDay(at string) (hour, minute int, err error) {
	t, err := time.Parse("15:04", at)
	if err != nil {
		return 0, 0, ErrBadTime
	}
	return t.Hour(), t.Minute(), nil
}
