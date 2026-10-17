package user

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

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
	userColumns   = `id, username, role, theme, display_name, email, avatar, blocked_at, created_at, access_role_id`
	apiKeyColumns = `id, user_id, key_hash, name, access_role_id, created_at, last_used_at`
)

// joinedUserColumns is userColumns under the alias `u`, for the two
// queries that reach a user through another table.
//
// Derived rather than written out, and the reason is what happened when
// they were: both spelled the list themselves, so adding a column to
// `users` left them selecting one fewer than scanUser reads — and
// because those two queries are how an **API key and a session cookie
// resolve to a person**, every request on the instance answered 401.
// A third list is a third place to forget.
var joinedUserColumns = qualify("u", userColumns)

// qualify prefixes every name in a comma-separated select list with a
// table alias.
func qualify(alias, list string) string {
	names := strings.Split(list, ",")
	for i, name := range names {
		names[i] = alias + "." + strings.TrimSpace(name)
	}
	return strings.Join(names, ", ")
}

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (*User, error) { return scanUserWith(row) }

// scanUserWith is scanUser plus whatever a query selected after the
// user's own columns — a password hash, so far.
//
// One scanner rather than two, because `userColumns` is read in order
// and a second copy of that order is a second place to forget when a
// column moves. It already went wrong twice; this is the shape that
// stops it going wrong a third time.
func scanUserWith(row scanner, extra ...any) (*User, error) {
	var u User
	var role sql.NullInt64
	dest := []any{
		&u.ID, &u.Username, &u.Role, &u.Theme,
		&u.DisplayName, &u.Email, &u.Avatar, &u.BlockedAt, &u.CreatedAt, &role,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	u.AccessRoleID = role.Int64
	return &u, nil
}

// UpdateProfile writes what an account says about its holder, the
// username included.
//
// One statement with every field, because they are saved from one form
// and a partial write would be a form that half took. The username is
// in here rather than in a method of its own for the same reason — it
// is a field on that form, and the rule that makes it special is the
// service's to apply, not this one's.
func (r *Repository) UpdateProfile(ctx context.Context, userID int64, u *User, updateAvatar ...bool) (*User, error) {
	changeAvatar := len(updateAvatar) == 0 || updateAvatar[0]
	row := r.q.QueryRowContext(ctx,
		`UPDATE users SET username = $2, display_name = $3, email = $4, avatar = CASE WHEN $6 THEN $5 ELSE avatar END
		 WHERE id = $1 RETURNING `+userColumns,
		userID, u.Username, u.DisplayName, u.Email, u.Avatar, changeAvatar)
	updated, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return updated, nil
}

// SetTheme records which palette this person sees the dashboard in.
func (r *Repository) SetTheme(ctx context.Context, userID int64, theme string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE users SET theme = $2 WHERE id = $1`, userID, theme); err != nil {
		return fmt.Errorf("record the theme: %w", err)
	}
	return nil
}

func scanAPIKey(row scanner) (*APIKey, error) {
	var k APIKey
	var role sql.NullInt64
	if err := row.Scan(&k.ID, &k.UserID, &k.KeyHash, &k.Name, &role, &k.CreatedAt, &k.LastUsedAt); err != nil {
		return nil, err
	}
	k.AccessRoleID = role.Int64
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
// refuseIfLastAdmin is the one rule three different acts ask for.
//
// Deleting an account, demoting it and blocking it are three ways of
// taking the last admin off an instance, and an instance with no admin
// can never configure itself again — setup closed the moment the first
// account existed, so nothing in the API can put one back. Deleting was
// the only one that existed and it counted admins inline; the other two
// would have been two more copies of the same count with two more
// chances to get the boundary wrong.
//
// `loses` is whether the act being asked for costs the instance this
// admin. Demoting to member does, promoting does not, and blocking is
// the same question read the other way — which is why the caller says
// so rather than this guessing from what it is handed.
//
// Counted here rather than before the transaction, because two requests
// each taking one of the last two admins would both pass a check made
// outside one.
func (r *Repository) refuseIfLastAdmin(ctx context.Context, target *User, loses bool) error {
	if !loses || target.Role != RoleAdmin {
		return nil
	}
	admins, err := r.CountByRole(ctx, RoleAdmin)
	if err != nil {
		return err
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// SetRole moves an account between admin and member. Which roles may
// be set, and whether this one may lose the last admin, is the
// service's to decide — this writes what it was told.
func (r *Repository) SetRole(ctx context.Context, id int64, role Role) error {
	_, err := r.q.ExecContext(ctx, `UPDATE users SET role = $1 WHERE id = $2`, string(role), id)
	if err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	return nil
}

// SetBlocked shuts an account out, or lets it back in.
//
// The timestamp is the database's own rather than one this process
// computed: the daemon and Postgres are two containers with two clocks,
// and every other moment recorded here comes from the same place.
func (r *Repository) SetBlocked(ctx context.Context, id int64, blocked bool) error {
	var err error
	if blocked {
		// Written only when it is not already set, so blocking an
		// account twice does not move the date the first one happened.
		_, err = r.q.ExecContext(ctx,
			`UPDATE users SET blocked_at = now() WHERE id = $1 AND blocked_at IS NULL`, id)
	} else {
		_, err = r.q.ExecContext(ctx, `UPDATE users SET blocked_at = NULL WHERE id = $1`, id)
	}
	if err != nil {
		return fmt.Errorf("set blocked: %w", err)
	}
	return nil
}

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
	return r.CreateRoleAPIKey(ctx, userID, keyHash, name, 0)
}

// CreateRoleAPIKey issues a key narrowed to an access role; 0 is none.
func (r *Repository) CreateRoleAPIKey(ctx context.Context, userID int64, keyHash, name string, roleID int64) (*APIKey, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO api_keys (user_id, key_hash, name, access_role_id) VALUES ($1, $2, $3, $4) RETURNING `+apiKeyColumns,
		userID, keyHash, name, nullID(roleID))
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

// nullID is an optional foreign key: 0 is none.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// ByAPIKeyHash resolves a credential to the identity that holds it. This
// is the authentication query.
func (r *Repository) ByAPIKeyHash(ctx context.Context, keyHash string) (*User, error) {
	row := r.q.QueryRowContext(ctx, `
		SELECT `+joinedUserColumns+`
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
