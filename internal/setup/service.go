package setup

import (
	"context"
	"fmt"

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
}

// Claim creates the instance's first account: an admin, with the
// password they chose.
//
// Nothing else. There is no organization to invent any more, and a
// project is something you create when you have something to put in it —
// a slug is permanent, so a name picked on someone's behalf is one they
// are stuck with.
//
// The count and the insert are one transaction. A half-finished setup
// would be unrecoverable through the API: the user exists, so setup
// refuses to run again.
func (s *Service) Claim(ctx context.Context, username, password string) (*Result, error) {
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}
	if !slug.Valid(username) {
		return nil, fmt.Errorf("username %w", slug.ErrInvalid)
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

		created, err := s.users.CreateWithPassword(ctx, tx, username, password, user.RoleAdmin)
		if err != nil {
			return err
		}
		result.User = created
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
