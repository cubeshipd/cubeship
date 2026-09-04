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

func (s *Store) CreateAPIKey(ctx context.Context, userID int64, keyHash string) (*APIKey, error) {
	res, err := s.db.ExecContext(ctx,
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

func (s *Store) GetUserByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.is_super_admin, u.created_at
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		WHERE k.key_hash = ?`, keyHash)
	u, err := s.scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by api key: %w", err)
	}
	return u, nil
}

func (s *Store) RevokeAPIKeysForUser(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("revoke api keys: %w", err)
	}
	return nil
}

func (s *Store) TouchAPIKeyLastUsed(ctx context.Context, keyHash string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE key_hash = ?`, keyHash); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}
