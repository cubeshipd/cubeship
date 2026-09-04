package store

import (
	"context"
	"fmt"
	"time"
)

type User struct {
	ID           int64
	Username     string
	IsSuperAdmin bool
	CreatedAt    time.Time
}

func createUser(ctx context.Context, q queryer, username string, isSuperAdmin bool) (*User, error) {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO users (username, is_super_admin) VALUES (?, ?)`, username, isSuperAdmin); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return getUserByUsername(ctx, q, username)
}

func (s *Store) CreateUser(ctx context.Context, username string, isSuperAdmin bool) (*User, error) {
	return createUser(ctx, s.db, username, isSuperAdmin)
}

// CreateUser is CreateUser inside t's transaction.
func (t *Tx) CreateUser(ctx context.Context, username string, isSuperAdmin bool) (*User, error) {
	return createUser(ctx, t.q, username, isSuperAdmin)
}

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.IsSuperAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func getUserByUsername(ctx context.Context, q queryer, username string) (*User, error) {
	row := q.QueryRowContext(ctx,
		`SELECT id, username, is_super_admin, created_at FROM users WHERE username = ?`, username)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", username, err)
	}
	return u, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return getUserByUsername(ctx, s.db, username)
}

// GetUserByUsername is GetUserByUsername inside t's transaction. A user
// that does not exist comes back as an error wrapping ErrNotFound.
func (t *Tx) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return getUserByUsername(ctx, t.q, username)
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, username, is_super_admin, created_at FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}
