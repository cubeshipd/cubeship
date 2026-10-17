package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cubeship/internal/platform/database"
)

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
// account has one at all. Every account made through the API has one
// from the moment it exists; an account with none is one made before
// this instance issued them, or straight through this repository.
func (r *Repository) PasswordHash(ctx context.Context, username string) (*User, string, error) {
	var hash string
	row := r.q.QueryRowContext(ctx,
		`SELECT `+userColumns+`, COALESCE(password_hash, '') FROM users WHERE username = $1`, username)

	// Scanned through the shared function rather than by hand, with
	// the one extra column passed in. Written out, this was the third
	// place a `users` column had to be added and the third that was
	// missed — and this one is the sign-in query, so what it looked
	// like was every password on the instance being wrong.
	u, err := scanUserWith(row, &hash)
	if err != nil {
		return nil, "", fmt.Errorf("get user %q: %w", username, err)
	}
	return u, hash, nil
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
		SELECT `+joinedUserColumns+`
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

// --- access roles ---

const roleColumns = `id, name, description, grants, COALESCE(system, ''), created_at, updated_at,
	(SELECT COUNT(*) FROM users u WHERE u.access_role_id = access_roles.id),
	(SELECT COUNT(*) FROM api_keys k WHERE k.access_role_id = access_roles.id)`

func scanRole(row scanner) (*AccessRole, error) {
	var ar AccessRole
	var grants []byte
	if err := row.Scan(&ar.ID, &ar.Name, &ar.Description, &grants, &ar.System, &ar.CreatedAt, &ar.UpdatedAt, &ar.Members, &ar.Keys); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(grants, &ar.Grants); err != nil {
		return nil, fmt.Errorf("read the grants of role %d: %w", ar.ID, err)
	}
	return &ar, nil
}

func (r *Repository) ListRoles(ctx context.Context) ([]*AccessRole, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+roleColumns+` FROM access_roles ORDER BY system IS NULL, id`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	out := []*AccessRole{}
	for rows.Next() {
		ar, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ar)
	}
	return out, rows.Err()
}

func (r *Repository) RoleByID(ctx context.Context, id int64) (*AccessRole, error) {
	ar, err := scanRole(r.q.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM access_roles WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoSuchRole
	}
	return ar, err
}

func (r *Repository) CreateRole(ctx context.Context, name, description string, grants []Grant) (int64, error) {
	raw, err := json.Marshal(grants)
	if err != nil {
		return 0, err
	}
	var id int64
	err = r.q.QueryRowContext(ctx,
		`INSERT INTO access_roles (name, description, grants) VALUES ($1, $2, $3) RETURNING id`,
		name, description, raw).Scan(&id)
	return id, err
}

func (r *Repository) UpdateRole(ctx context.Context, id int64, name, description string, grants []Grant) error {
	raw, err := json.Marshal(grants)
	if err != nil {
		return err
	}
	res, err := r.q.ExecContext(ctx,
		`UPDATE access_roles SET name = $2, description = $3, grants = $4, updated_at = now() WHERE id = $1`,
		id, name, description, raw)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSuchRole
	}
	return nil
}

func (r *Repository) DeleteRole(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM access_roles WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoSuchRole
	}
	return nil
}

// SystemRoleID is the id of a role the instance ships.
func (r *Repository) SystemRoleID(ctx context.Context, system string) (int64, error) {
	var id int64
	err := r.q.QueryRowContext(ctx, `SELECT id FROM access_roles WHERE system = $1`, system).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoSuchRole
	}
	return id, err
}

// SetAccessRole gives an account a role; 0 is none, the member default.
func (r *Repository) SetAccessRole(ctx context.Context, userID, roleID int64) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE users SET access_role_id = $2 WHERE id = $1`, userID, nullID(roleID)); err != nil {
		return fmt.Errorf("give the role: %w", err)
	}
	return nil
}

// SetAvatar locks the identity before replacing its bytes. Both changes belong
// to one statement, including concurrent uploads and deletes for this account.
func (r *Repository) SetAvatar(ctx context.Context, id int64, version, mediaType string, data []byte) error {
	_, err := r.q.ExecContext(ctx, `WITH account AS (
		UPDATE users SET avatar = $2 WHERE id = $1 RETURNING id
	) INSERT INTO user_avatars (user_id, media_type, image)
	SELECT id, $3, $4 FROM account
	ON CONFLICT (user_id) DO UPDATE SET media_type = EXCLUDED.media_type, image = EXCLUDED.image`, id, version, mediaType, data)
	if err != nil {
		return fmt.Errorf("save profile image: %w", err)
	}
	return nil
}

func (r *Repository) Avatar(ctx context.Context, username string) ([]byte, string, string, error) {
	var data []byte
	var mediaType, version string
	err := r.q.QueryRowContext(ctx, `SELECT a.image, a.media_type, u.avatar
		FROM user_avatars a JOIN users u ON u.id = a.user_id
		WHERE u.username = $1 AND u.avatar LIKE 'upload:%'`, username).Scan(&data, &mediaType, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", "", ErrNoAvatar
	}
	if err != nil {
		return nil, "", "", fmt.Errorf("read profile image: %w", err)
	}
	return data, mediaType, version, nil
}

func (r *Repository) ClearAvatar(ctx context.Context, id int64) error {
	_, err := r.q.ExecContext(ctx, `WITH account AS (
		UPDATE users SET avatar = '' WHERE id = $1 RETURNING id
	) DELETE FROM user_avatars USING account WHERE user_avatars.user_id = account.id`, id)
	if err != nil {
		return fmt.Errorf("remove profile image: %w", err)
	}
	return nil
}
