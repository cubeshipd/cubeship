package user

import (
	"context"
	"fmt"
	"time"

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
	userColumns   = `id, username, role, created_at`
	apiKeyColumns = `id, user_id, key_hash, name, created_at, last_used_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
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

func (r *Repository) Create(ctx context.Context, username string, role Role) (*User, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO users (username, role) VALUES ($1, $2) RETURNING `+userColumns,
		username, string(role))
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

// List returns every account, ordered by name. There is one instance
// and no tenant boundary, so this is the whole of it.
func (r *Repository) List(ctx context.Context) ([]*User, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountByRole is what the last-admin rule asks: an instance with no
// admin left can never configure itself again, and nothing in the API
// could put one back.
func (r *Repository) CountByRole(ctx context.Context, role Role) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = $1`, string(role)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s users: %w", role, err)
	}
	return n, nil
}

// Delete removes an account. Its keys and sessions reference it, so they
// go first — see Service.Delete, which does all three in one
// transaction.
func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// DeleteAPIKeys revokes every key one account holds, and reports how
// many there were.
func (r *Repository) DeleteAPIKeys(ctx context.Context, userID int64) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke api keys: %w", err)
	}
	return res.RowsAffected()
}

// DeleteSessions ends every session one account holds, and reports how
// many there were.
func (r *Repository) DeleteSessions(ctx context.Context, userID int64) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("end sessions: %w", err)
	}
	return res.RowsAffected()
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
		SELECT u.id, u.username, u.role, u.created_at
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

// --- passwords ---

// SetPassword stores an already-hashed password. The hashing happens in
// the service; a repository must never be handed a plaintext one.
func (r *Repository) SetPassword(ctx context.Context, userID int64, hash string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, userID); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

// PasswordHash returns the stored hash for a username, and whether the
// account has one at all. An account created by an organization admin
// has an API key immediately and a password only once it sets one.
func (r *Repository) PasswordHash(ctx context.Context, username string) (*User, string, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+userColumns+`, COALESCE(password_hash, '') FROM users WHERE username = $1`, username)

	var u User
	var hash string
	if err := row.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &hash); err != nil {
		return nil, "", fmt.Errorf("get user %q: %w", username, err)
	}
	return &u, hash, nil
}

// HasPassword reports whether an account can sign in with one.
func (r *Repository) HasPassword(ctx context.Context, userID int64) (bool, error) {
	var has bool
	if err := r.q.QueryRowContext(ctx,
		`SELECT password_hash IS NOT NULL FROM users WHERE id = $1`, userID).Scan(&has); err != nil {
		return false, fmt.Errorf("check password: %w", err)
	}
	return has, nil
}

// --- sessions ---

const sessionColumns = `token_hash, user_id, created_at, expires_at, last_used_at`

func (r *Repository) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) (*Session, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3) RETURNING `+sessionColumns,
		tokenHash, userID, expiresAt)

	var s Session
	if err := row.Scan(&s.TokenHash, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.LastUsedAt); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

// UserBySession resolves a live session to whoever holds it. An expired
// row resolves to nothing: the deletion pass is housekeeping, not the
// thing that makes expiry take effect.
func (r *Repository) UserBySession(ctx context.Context, tokenHash string) (*User, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.role, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("get user by session: %w", err)
	}
	return u, nil
}

func (r *Repository) TouchSession(ctx context.Context, tokenHash string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE sessions SET last_used_at = now() WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteOtherSessions ends every session a user holds except the one
// given. Changing a password does this: whoever knew the old one should
// not stay signed in.
func (r *Repository) DeleteOtherSessions(ctx context.Context, userID int64, keepTokenHash string) error {
	if _, err := r.q.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keepTokenHash); err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes rows nobody can use any more.
func (r *Repository) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return res.RowsAffected()
}
