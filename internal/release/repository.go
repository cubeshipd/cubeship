package release

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository is every SQL statement about what somebody has been shown.
//
// The notes themselves are not in here and never will be: they are in
// the binary. What a database can answer is the one thing that differs
// per instance and per person — how far each of them has read.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// Seen is the newest release this person has been shown, or empty for
// somebody who has never been shown anything.
func (r *Repository) Seen(ctx context.Context, userID int64) (string, error) {
	var v string
	err := r.q.QueryRowContext(ctx,
		`SELECT version FROM release_seen WHERE user_id = $1`, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read which release was seen: %w", err)
	}
	return v, nil
}

// MarkSeen records that this person has read up to version.
//
// Whether that is *forward* is decided above this, in the service,
// where the version comparison lives: ordering semver in SQL is a
// string comparison that gets 0.10.0 wrong, and there is already one
// function in this package that gets it right.
func (r *Repository) MarkSeen(ctx context.Context, userID int64, version string) error {
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO release_seen (user_id, version) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET version = $2, seen_at = now()`,
		userID, version); err != nil {
		return fmt.Errorf("record which release was seen: %w", err)
	}
	return nil
}
