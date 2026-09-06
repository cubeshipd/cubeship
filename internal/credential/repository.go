package credential

import (
	"context"
	"database/sql"
	"fmt"

	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

// columns is read in order by scan. Change one, change both.
const columns = `id, provider, label, username, password, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Credential, error) {
	var c Credential
	if err := row.Scan(&c.ID, &c.Provider, &c.Label, &c.Username, &c.Password,
		&c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) Create(ctx context.Context, c *Credential) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO credentials (provider, label, username, password)
		 VALUES ($1, $2, $3, $4) RETURNING `+columns,
		string(c.Provider), c.Label, c.Username, c.Password)
	created, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create credential: %w", err)
	}
	return created, nil
}

// Update changes the label and the secret. A nil argument leaves the
// column alone, so renaming one cannot blank its password.
//
// Not the provider: what a credential is for is what its secret is, and
// a provider changed under a stored secret would be a secret sent to
// somebody it was never issued for.
func (r *Repository) Update(ctx context.Context, id int64, label, username, password *string) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`UPDATE credentials SET
		   label = COALESCE($1, label),
		   username = COALESCE($2, username),
		   password = COALESCE($3, password),
		   updated_at = now()
		 WHERE id = $4 RETURNING `+columns,
		label, username, password, id)
	c, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update credential: %w", err)
	}
	return c, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Credential, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM credentials WHERE id = $1`, id)
	c, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get credential %d: %w", id, err)
	}
	return c, nil
}

// List is every credential, oldest first — the order they were added is
// the order somebody remembers adding them.
func (r *Repository) List(ctx context.Context) ([]*Credential, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+columns+` FROM credentials ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func scanAll(rows *sql.Rows) ([]*Credential, error) {
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

func (r *Repository) Delete(ctx context.Context, id int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM credentials WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}
