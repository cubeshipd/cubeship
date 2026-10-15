package settings

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

// All reads every setting. There are a handful, read on nearly every
// boot and on any change, so they are always fetched together.
func (r *Repository) All(ctx context.Context) (Values, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	defer rows.Close()

	out := Values{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// Set writes one setting, replacing whatever was there.
func (r *Repository) Set(ctx context.Context, key, value string) error {
	if _, err := r.q.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, value); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

// SetIfUnset writes a setting only when it has never been set, and
// reports whether it wrote. It is how an environment variable from an
// older release seeds the database exactly once, without overwriting
// what an operator has since changed in the dashboard.
func (r *Repository) SetIfUnset(ctx context.Context, key, value string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO NOTHING`,
		key, value)
	if err != nil {
		return false, fmt.Errorf("seed setting %q: %w", key, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
