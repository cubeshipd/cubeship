package store

import (
	"context"
	"fmt"
	"time"
)

type APIKey struct {
	ID         int64
	UserID     int64
	KeyHash    string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

func createAPIKey(ctx context.Context, q queryer, userID int64, keyHash string) (*APIKey, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash) VALUES (?, ?)`, userID, keyHash)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &APIKey{ID: id, UserID: userID, KeyHash: keyHash}, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, keyHash string) (*APIKey, error) {
	return createAPIKey(ctx, s.db, userID, keyHash)
}

// CreateAPIKey is CreateAPIKey inside t's transaction.
func (t *Tx) CreateAPIKey(ctx context.Context, userID int64, keyHash string) (*APIKey, error) {
	return createAPIKey(ctx, t.q, userID, keyHash)
}

func (s *Store) GetUserByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_super_admin, u.created_at
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ?`, keyHash)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by api key: %w", err)
	}
	return u, nil
}

func revokeAPIKeysForUser(ctx context.Context, q queryer, userID int64) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke api keys: %w", err)
	}
	return nil
}

func (s *Store) RevokeAPIKeysForUser(ctx context.Context, userID int64) error {
	return revokeAPIKeysForUser(ctx, s.db, userID)
}

// RevokeAPIKeysForUser is RevokeAPIKeysForUser inside t's transaction.
// Rotation pairs it with CreateAPIKey so a failure between the two can't
// leave the user with no key at all.
func (t *Tx) RevokeAPIKeysForUser(ctx context.Context, userID int64) error {
	return revokeAPIKeysForUser(ctx, t.q, userID)
}

func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, keyHash string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`, keyHash); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
