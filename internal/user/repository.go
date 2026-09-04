package user

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes users and their API keys. It is a thin
// value over a Queryer, so the same code runs on the pool or inside a
// transaction: NewRepository(tx) inside database.WithTx.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const (
	userColumns   = `id, username, is_super_admin, created_at`
	apiKeyColumns = `id, user_id, key_hash, name, created_at, last_used_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.IsSuperAdmin, &u.CreatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

func scanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.CreatedAt, &k.LastUsedAt); err != nil {
		return nil, err
	}
	return &k, nil
}

func (r *Repository) Create(ctx context.Context, username string, isSuperAdmin bool) (*User, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO users (username, is_super_admin) VALUES ($1, $2) RETURNING `+userColumns,
		username, isSuperAdmin)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (r *Repository) ByUsername(ctx context.Context, username string) (*User, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE username = $1`, username)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %q: %w", username, err)
	}
	return u, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*User, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user %d: %w", id, err)
	}
	return u, nil
}

func (r *Repository) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func (r *Repository) CreateAPIKey(ctx context.Context, userID int64, keyHash, name string) (*APIKey, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash, name) VALUES ($1, $2, $3) RETURNING `+apiKeyColumns,
		userID, keyHash, name)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

// ByAPIKeyHash resolves a credential to the identity that holds it. This
// is the authentication query.
func (r *Repository) ByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_super_admin, u.created_at
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = $1`, keyHash)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by api key: %w", err)
	}
	return u, nil
}

func (r *Repository) APIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE key_hash = $1`, keyHash)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return k, nil
}

func (r *Repository) ListAPIKeys(ctx context.Context, userID int64) ([]*APIKey, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+apiKeyColumns+` FROM api_keys WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKeyByHash revokes exactly the key with this hash — the one a
// caller is currently authenticated with, for instance — leaving every
// other key that same user holds untouched.
func (r *Repository) RevokeAPIKeyByHash(ctx context.Context, keyHash string) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM api_keys WHERE key_hash = $1`, keyHash); err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// RevokeAPIKeyByID deletes the key with the given id, scoped to userID so
// one user can never revoke another user's key by guessing an id. Returns
// database.ErrNotFound if id doesn't exist or doesn't belong to userID.
func (r *Repository) RevokeAPIKeyByID(ctx context.Context, id, userID int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

func (r *Repository) TouchAPIKeyLastUsed(ctx context.Context, keyHash string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE key_hash = $1`, keyHash); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
