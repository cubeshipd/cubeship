package bitbucket

import (
	"context"
	"cubeship/internal/platform/database"
	"errors"
	"time"
)

type Repo struct{ q database.Queryer }

func NewRepository(q database.Queryer) *Repo { return &Repo{q} }

const columns = "id, account_name, account_uuid, user_id, created_at, access_token, refresh_token, expires_at, consumer_id"

func scan(row interface{ Scan(...any) error }) (*Connection, error) {
	c := new(Connection)
	err := row.Scan(&c.ID, &c.Account, &c.UUID, &c.UserID, &c.CreatedAt, &c.accessToken, &c.refreshToken, &c.expiresAt, &c.consumerID)
	return c, err
}
func (r *Repo) List(ctx context.Context) ([]*Connection, error) {
	rows, err := r.q.QueryContext(ctx, "SELECT "+columns+" FROM bitbucket_connections ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Connection, 0)
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r *Repo) Lock(ctx context.Context, id int64) (*Connection, error) {
	return scan(r.q.QueryRowContext(ctx, "SELECT "+columns+" FROM bitbucket_connections WHERE id=$1 FOR UPDATE", id))
}
func (r *Repo) Save(ctx context.Context, c *Connection) (*Connection, error) {
	return scan(r.q.QueryRowContext(ctx, `INSERT INTO bitbucket_connections (user_id,account_uuid,account_name,consumer_id,access_token,refresh_token,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (account_uuid) DO UPDATE SET user_id=EXCLUDED.user_id,account_name=EXCLUDED.account_name,consumer_id=EXCLUDED.consumer_id,access_token=EXCLUDED.access_token,refresh_token=EXCLUDED.refresh_token,expires_at=EXCLUDED.expires_at RETURNING `+columns, c.UserID, c.UUID, c.Account, c.consumerID, c.accessToken, c.refreshToken, c.expiresAt))
}
func (r *Repo) UpdateTokens(ctx context.Context, c *Connection) error {
	_, err := r.q.ExecContext(ctx, "UPDATE bitbucket_connections SET access_token=$2,refresh_token=$3,expires_at=$4 WHERE id=$1", c.ID, c.accessToken, c.refreshToken, c.expiresAt)
	return err
}
func (r *Repo) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, "DELETE FROM bitbucket_connections WHERE id=$1", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return database.ErrNotFound
	}
	return err
}
func (r *Repo) IssueState(ctx context.Context, hash string, userID int64, consumerID string, expires, now time.Time) error {
	_, err := r.q.ExecContext(ctx, "DELETE FROM bitbucket_oauth_states WHERE expires_at <= $1", now)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, "INSERT INTO bitbucket_oauth_states(state_hash,user_id,consumer_id,expires_at) VALUES($1,$2,$3,$4)", hash, userID, consumerID, expires)
	return err
}
func (r *Repo) ConsumeState(ctx context.Context, hash string, userID int64, consumerID string, now time.Time) error {
	var found string
	err := r.q.QueryRowContext(ctx, "DELETE FROM bitbucket_oauth_states WHERE state_hash=$1 AND user_id=$2 AND consumer_id=$3 AND expires_at>$4 RETURNING state_hash", hash, userID, consumerID, now).Scan(&found)
	if errors.Is(err, database.ErrNotFound) {
		return ErrState
	}
	return err
}
