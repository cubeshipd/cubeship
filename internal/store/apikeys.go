package store

import (
	"context"
	"fmt"
	"time"
)

// DefaultAPIKeyName is the name given to a key created without one
// explicitly chosen: the super-admin's bootstrap key and a new org
// user's first key. A key created via the API's "additional key"
// endpoint (see api.handleCreateAPIKey) always carries a caller-chosen
// name instead — "mcp", "laptop", whatever distinguishes it.
const DefaultAPIKeyName = "default"

type APIKey struct {
	ID         int64
	UserID     int64
	KeyHash    string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

const apiKeyColumns = `id, user_id, key_hash, name, created_at, last_used_at`

func scanAPIKey(row interface{ Scan(dest ...any) error }) (*APIKey, error) {
	var k APIKey
	if err := row.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &k.CreatedAt, &k.LastUsedAt); err != nil {
		return nil, err
	}
	return &k, nil
}

func createAPIKey(ctx context.Context, q queryer, userID int64, keyHash, name string) (*APIKey, error) {
	row := q.QueryRowContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash, name) VALUES ($1, $2, $3) RETURNING `+apiKeyColumns,
		userID, keyHash, name)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, keyHash, name string) (*APIKey, error) {
	return createAPIKey(ctx, s.db, userID, keyHash, name)
}

// CreateAPIKey is CreateAPIKey inside t's transaction.
func (t *Tx) CreateAPIKey(ctx context.Context, userID int64, keyHash, name string) (*APIKey, error) {
	return createAPIKey(ctx, t.q, userID, keyHash, name)
}

func (s *Store) GetUserByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
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

func (s *Store) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM api_keys WHERE key_hash = $1`, keyHash)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return k, nil
}

func (s *Store) ListAPIKeysForUser(ctx context.Context, userID int64) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
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

func revokeAPIKeyByHash(ctx context.Context, q queryer, keyHash string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM api_keys WHERE key_hash = $1`, keyHash); err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return nil
}

// RevokeAPIKeyByHash revokes exactly the key with this hash — the one a
// caller is currently authenticated with, for instance — leaving every
// other key that same user holds untouched. See RevokeAPIKeyByID for
// revoking a key the caller isn't currently using.
func (s *Store) RevokeAPIKeyByHash(ctx context.Context, keyHash string) error {
	return revokeAPIKeyByHash(ctx, s.db, keyHash)
}

// RevokeAPIKeyByHash is RevokeAPIKeyByHash inside t's transaction.
func (t *Tx) RevokeAPIKeyByHash(ctx context.Context, keyHash string) error {
	return revokeAPIKeyByHash(ctx, t.q, keyHash)
}

// RevokeAPIKeyByID deletes the key with the given id, scoped to userID so
// one user can never revoke another user's key by guessing an id. Returns
// ErrNotFound if id doesn't exist or doesn't belong to userID.
func (s *Store) RevokeAPIKeyByID(ctx context.Context, id, userID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, keyHash string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE key_hash = $1`, keyHash); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
