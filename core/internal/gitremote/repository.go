package gitremote

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

type Repo struct{ q database.Queryer }

func NewRepository(q database.Queryer) *Repo { return &Repo{q} }

const columns = "id, provider, base_url, account_id, account, api_token, webhook_secret, user_id, created_at"

func scan(row interface{ Scan(...any) error }) (*Connection, error) {
	c := new(Connection)
	err := row.Scan(&c.ID, &c.Provider, &c.BaseURL, &c.accountID, &c.Account, &c.token, &c.secret, &c.UserID, &c.CreatedAt)
	return c, err
}

func (r *Repo) Save(ctx context.Context, c *Connection) (*Connection, error) {
	return scan(r.q.QueryRowContext(ctx, `INSERT INTO git_remote_connections
		(provider, base_url, account_id, account, api_token, webhook_secret, user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (provider, base_url, account_id) DO UPDATE SET
		account=EXCLUDED.account, api_token=EXCLUDED.api_token,
		user_id=EXCLUDED.user_id
		RETURNING `+columns, c.Provider, c.BaseURL, c.accountID, c.Account, c.token, c.secret, c.UserID))
}

func (r *Repo) List(ctx context.Context) ([]*Connection, error) {
	rows, err := r.q.QueryContext(ctx, "SELECT "+columns+" FROM git_remote_connections ORDER BY provider, base_url, account")
	if err != nil {
		return nil, fmt.Errorf("list Git connections: %w", err)
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

func (r *Repo) Get(ctx context.Context, id int64) (*Connection, error) {
	return scan(r.q.QueryRowContext(ctx, "SELECT "+columns+" FROM git_remote_connections WHERE id=$1", id))
}

func (r *Repo) Delete(ctx context.Context, id int64) error {
	result, err := r.q.ExecContext(ctx, "DELETE FROM git_remote_connections WHERE id=$1", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return database.ErrNotFound
	}
	return err
}
