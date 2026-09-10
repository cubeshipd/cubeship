package release

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service answers what this instance is running and what changed in it.
//
// The notes come from the binary and the "how far have I read" comes
// from the database, which is the whole of the module: there is nothing
// to configure, nothing to provision and nothing to reach.
type Service struct {
	db *database.DB
	// version is what this build is, stamped at link time. A build with
	// nothing stamped on it — `make dev`, or a binary somebody built by
	// hand — has no release to be on, and says so by having nothing to
	// show.
	version string
	// notes is read once at startup rather than per request: it is a
	// handful of files in the binary and they cannot change while it
	// runs.
	notes []Note
	// broken is why they could not be read, kept rather than returned
	// so that constructing a server stays something that cannot fail.
	//
	// It surfaces at the endpoint, with the parser's own words, instead
	// of an empty dialog nobody would think to question — and it cannot
	// reach a release anyway: TestEveryNoteInThisBuildParses reads the
	// same files in CI.
	broken error
}

func NewService(db *database.DB, version string) *Service {
	notes, err := All()
	return &Service{db: db, version: Normalize(version), notes: notes, broken: err}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Version is what this instance is running, without a leading v. Empty
// on a build with nothing stamped on it.
func (s *Service) Version() string { return s.version }

// State is what a person has and has not read.
type State struct {
	// Version is what this instance is running.
	Version string
	// Notes is every release up to that version, newest first — so a
	// screen can offer the history and not only the news.
	Notes []Note
	// Unseen is the ones this caller has not been shown, newest first.
	// Empty is the ordinary answer, and it is what a dialog reads to
	// decide not to appear.
	Unseen []Note
}

// For is that state for one person.
//
// **A member's, not an admin's.** What changed in the software everyone
// on this instance is using is not a fact about its configuration, and
// a release note nobody but the admin may read would be one the person
// who noticed the change cannot look up.
func (s *Service) For(ctx context.Context, caller *user.User) (State, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return State{}, err
	}
	if s.broken != nil {
		return State{}, fmt.Errorf("this build's release notes could not be read: %w", s.broken)
	}
	out := State{Version: s.version}
	for _, n := range s.notes {
		if s.version == "" || Compare(n.Version, s.version) <= 0 {
			out.Notes = append(out.Notes, n)
		}
	}
	seen, err := s.Repo().Seen(ctx, caller.ID)
	if err != nil {
		return State{}, err
	}
	out.Unseen = Since(s.notes, seen, s.version)
	return out, nil
}

// MarkSeen records that this person has read up to what is running.
//
// **It only ever moves forward.** A second tab, or a request that
// arrives late, would otherwise write an older version over a newer one
// and show the same notes again — which is the one thing this exists to
// stop.
//
// It is a no-op on a build with no version: there is nothing to have
// read.
func (s *Service) MarkSeen(ctx context.Context, caller *user.User) error {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return err
	}
	if s.version == "" {
		return nil
	}
	seen, err := s.Repo().Seen(ctx, caller.ID)
	if err != nil {
		return err
	}
	if Compare(seen, s.version) >= 0 {
		return nil
	}
	return s.Repo().MarkSeen(ctx, caller.ID, s.version)
}
