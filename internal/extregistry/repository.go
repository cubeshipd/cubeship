package extregistry

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes registry credentials. Like every other
// repository here it is a thin value over a Queryer, so the same code
// runs on the pool or inside a transaction.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const columns = `id, org_id, name, host, username, password, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Credential, error) {
	var c Credential
	if err := row.Scan(&c.ID, &c.OrgID, &c.Name, &c.Host, &c.Username, &c.Password,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) Create(ctx context.Context, orgID int64, name, host, username, password string) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO external_registries (org_id, name, host, username, password)
		 VALUES ($1, $2, $3, $4, $5) RETURNING `+columns,
		orgID, name, host, username, password)
	c, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create registry credential: %w", err)
	}
	return c, nil
}

func (r *Repository) Update(ctx context.Context, id, orgID int64, username, password string) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`UPDATE external_registries SET username = $1, password = $2, updated_at = now()
		 WHERE id = $3 AND org_id = $4 RETURNING `+columns,
		username, password, id, orgID)
	c, err := scan(row)
	if err != nil {
		return nil, database.ErrNotFound
	}
	return c, nil
}

func (r *Repository) List(ctx context.Context, orgID int64) ([]*Credential, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM external_registries WHERE org_id = $1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Credential
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ByHost is the deploy-time lookup: an image names a registry, and this
// answers whether the organization has a way in. A miss is not an error
// — a public image needs no credential.
func (r *Repository) ByHost(ctx context.Context, orgID int64, host string) (*Credential, bool, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM external_registries WHERE org_id = $1 AND host = $2`, orgID, host)
	c, err := scan(row)
	if err != nil {
		return nil, false, nil
	}
	return c, true, nil
}

func (r *Repository) Delete(ctx context.Context, id, orgID int64) error {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM external_registries WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		return fmt.Errorf("delete registry credential: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete registry credential: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}
