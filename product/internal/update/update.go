// Package update replaces this instance with a newer release, itself
// included.
//
// **The hard part is that the thing doing the work is what gets
// replaced.** The daemon runs as a container; updating it means
// stopping that container, which is the process running this code. So
// the last step is handed to a throwaway container started from the new
// image, which outlives the daemon it replaces — the same door
// `internal/platform/hostexec` uses for the firewall, used once more.
//
// **The state is a file, not a row.** Everything else here that
// remembers something uses Postgres, and this cannot: what it has to
// survive is the daemon going away, which is exactly when nothing can
// answer a query. The data directory is mounted at the same path inside
// the daemon's container and outside it, so the throwaway updater, the
// daemon that is going, and the daemon that comes back all read one
// file. `setup-token` already works this way.
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StatusFile is where the run is written down, under the data
// directory.
const StatusFile = "update.json"

// States a run can be in.
const (
	// StatusRunning is an update in progress. **Everything on the
	// instance is read-only while a run is in this state**, and it is
	// the reason the file exists: a browser reloaded mid-update reads
	// it and knows to stay out of the way, which no amount of client
	// state could tell it.
	StatusRunning = "running"
	// StatusDone is one that finished. Kept rather than deleted, so a
	// screen can say what it just did.
	StatusDone = "done"
	// StatusFailed is one that did not. Also kept: the reason is the
	// only thing anybody has to go on.
	StatusFailed = "failed"
)

// Run is one attempt to move this instance to a version.
type Run struct {
	// Version is what it is moving to, without a leading v.
	Version string `json:"version"`
	// From is what it was on, for a screen that says what changed.
	From   string `json:"from,omitempty"`
	Status string `json:"status"`
	// Step is what is happening now, in words somebody watching can
	// read: "updating eu-1", "pulling the new images".
	Step string `json:"step,omitempty"`
	// Done is every step that finished, oldest first. A progress bar
	// wants a count and a person wants the list, and this is both.
	Done []string `json:"done,omitempty"`
	// Error is why it failed, in whatever words the thing that failed
	// used.
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// Running reports whether this run is still going.
func (r *Run) Running() bool { return r != nil && r.Status == StatusRunning }

// Stale reports whether a running record has been running impossibly
// long.
//
// **A run that cannot end would lock the instance for ever**, and the
// one failure that could leave one behind is the throwaway updater
// dying between stopping the daemon and starting it — the one moment
// nothing is left to write the file. Nothing else can: every other step
// runs inside a process that records what went wrong.
//
// So a run older than this is read as over. It is deliberately long: an
// image pull on a slow box is minutes, and being wrong here means
// declaring an update finished while it is still going.
func (r *Run) Stale(after time.Duration) bool {
	return r.Running() && time.Since(r.StartedAt) > after
}

// StuckAfter is how long a running record is believed. See Stale.
const StuckAfter = 30 * time.Minute

// ErrInProgress refuses a second update while one is going.
var ErrInProgress = errors.New("an update is already running on this instance")

// ErrNotNewer refuses an update to where it already is.
var ErrNotNewer = errors.New("this instance is already on that release or a newer one")

// ErrUnknownVersion refuses a version that is not a release.
var ErrUnknownVersion = errors.New("no release by that name")

// ErrNotAContainer refuses to update a daemon that is not one.
//
// `make dev` runs the daemon as a host process, and replacing it would
// mean killing somebody's own build and starting a container in its
// place — which is not an upgrade, it is a surprise.
var ErrNotAContainer = errors.New("this daemon is not running as a container, so there is nothing to replace: it was built and started by hand")

// Store is the run, on disk.
//
// A whole-file read and a whole-file write, because it is a few hundred
// bytes and there is exactly one writer at a time: the daemon until it
// hands over, then the updater, then the daemon that comes back.
type Store struct{ DataDir string }

func (s Store) path() string { return filepath.Join(s.DataDir, StatusFile) }

// Read is the run on disk, or nil when there has never been one.
//
// A file that cannot be parsed is **nil with no error**, deliberately:
// the alternative is an instance that refuses every request because
// something wrote nonsense into a status file, which is the failure
// this whole mechanism exists to avoid.
func (s Store) Read() *Run {
	raw, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	var r Run
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil
	}
	return &r
}

// Write records the run.
//
// Written to a temporary file and renamed, because the reader may be
// another process: a rename is atomic, and a half-written status file
// is one that parses as nothing and unlocks an instance mid-update.
func (s Store) Write(r *Run) error {
	if s.DataDir == "" {
		return nil
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the update's status: %w", err)
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write the update's status: %w", err)
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		return fmt.Errorf("record the update's status: %w", err)
	}
	return nil
}

// Step records what is happening now, filing whatever was happening
// before under what is done.
func (s Store) Step(r *Run, step string) {
	if r.Step != "" {
		r.Done = append(r.Done, r.Step)
	}
	r.Step = step
	_ = s.Write(r)
}

// Finish closes the run, one way or the other.
func (s Store) Finish(r *Run, cause error) {
	now := time.Now()
	r.FinishedAt = &now
	if cause != nil {
		r.Status, r.Error = StatusFailed, cause.Error()
	} else {
		r.Status = StatusDone
		if r.Step != "" {
			r.Done, r.Step = append(r.Done, r.Step), ""
		}
	}
	_ = s.Write(r)
}
