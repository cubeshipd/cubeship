package dns

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes DNS credentials. Like every other
// repository here it is a thin value over a Queryer, so the same code
// runs on the pool or inside a transaction.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const columns = `id, provider, label, username, password, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Credential, error) {
	var c Credential
	if err := row.Scan(&c.ID, &c.Provider, &c.Label,
		&c.Username, &c.Password, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) Create(ctx context.Context, in Credential) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO dns_providers (provider, label, username, password)
		 VALUES ($1, $2, $3, $4) RETURNING `+columns,
		string(in.Provider), in.Label, in.Username, in.Password)
	c, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create DNS provider: %w", err)
	}
	return c, nil
}

// Update writes whichever of the three fields it was given.
//
// All three are pointers because "leave it alone" and "set it to empty"
// are different requests, and a nil is the only way to say the first
// one. Renaming a credential must not blank its token.
func (r *Repository) Update(ctx context.Context, id int64, label, username, password *string) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`UPDATE dns_providers
		 SET label    = COALESCE($1, label),
		     username = COALESCE($2, username),
		     password = COALESCE($3, password),
		     updated_at = now()
		 WHERE id = $4 RETURNING `+columns,
		label, username, password, id)
	c, err := scan(row)
	if err != nil {
		return nil, database.ErrNotFound
	}
	return c, nil
}

func (r *Repository) List(ctx context.Context) ([]*Credential, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM dns_providers ORDER BY label`)
	if err != nil {
		return nil, fmt.Errorf("list DNS providers: %w", err)
	}
	defer rows.Close()

	out := []*Credential{}
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (*Credential, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM dns_providers WHERE id = $1`, id)
	c, err := scan(row)
	if err != nil {
		return nil, database.ErrNotFound
	}
	return c, nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM dns_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete DNS provider: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return database.ErrNotFound
	}
	return nil
}
