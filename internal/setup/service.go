package setup

import (
	"context"
	"fmt"

	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// setupLock is the advisory lock key setup takes.
//
// Without it two requests arriving together would both count zero users
// and both succeed, leaving the instance with two super-admins and two
// organizations. The username's unique index does not help: they may
// well pick different names. Postgres holds this for the length of the
// transaction, so the second request waits, then finds the first one's
// user and is refused.
const setupLock = 0x00BE5117

type Service struct {
	db    *database.DB
	users *user.Service
}

func NewService(db *database.DB, users *user.Service) *Service {
	return &Service{db: db, users: users}
}

// Needed reports whether the instance is still unclaimed.
func (s *Service) Needed(ctx context.Context) (bool, error) {
	n, err := s.users.Repo().Count(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// Result is what claiming an instance produced.
type Result struct {
	User *user.User
	Org  *org.Organization
}

// Claim creates the instance's first account and the organization it
// works in: a super-admin, and the organization they named.
//
// No project. A project is a thing someone creates when they have
// something to put in it, and one invented here would be a row nobody
// asked for with a name nobody chose — permanent, since a slug never
// changes.
//
// Both halves in one transaction. A half-finished setup would be
// unrecoverable through the API — the user exists, so setup refuses to
// run again, but there is no organization to put anything in and no way
// to create one except as a super-admin who cannot sign in.
func (s *Service) Claim(ctx context.Context, username, password, orgSlug string) (*Result, error) {
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}
	if orgSlug == "" {
		return nil, ErrOrgRequired
	}
	if !slug.Valid(username) {
		return nil, fmt.Errorf("username %w", slug.ErrInvalid)
	}
	if slug.Reserved(orgSlug) {
		return nil, slug.ErrReserved
	}
	if !slug.Valid(orgSlug) {
		return nil, fmt.Errorf("organization %w", slug.ErrInvalid)
	}

	var result Result
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, setupLock); err != nil {
			return fmt.Errorf("take the setup lock: %w", err)
		}

		// Counted inside the lock, so a second request that was waiting
		// sees the first one's work.
		n, err := user.NewRepository(tx).Count(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrAlreadySetUp
		}

		created, err := s.users.CreateWithPassword(ctx, tx, username, password, true)
		if err != nil {
			return err
		}
		result.User = created

		orgs := org.NewRepository(tx)
		o, err := orgs.Create(ctx, orgSlug)
		if err != nil {
			return err
		}
		result.Org = o

		// The membership is not strictly needed — a super-admin is
		// authorized everywhere — but without it the organization would
		// not appear in their own list, which is where the dashboard
		// starts.
		return orgs.AddMembership(ctx, created.ID, o.ID, org.RoleAdmin)
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
