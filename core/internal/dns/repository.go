package dns

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes the DNS providers this instance reaches.
// Like every other repository here it is a thin value over a Queryer,
// so the same code runs on the pool or inside a transaction.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// A row holds which API to speak and which credential to speak it with.
// Every read joins that credential, so a provider client is handed one
// value with a login on it.
const columns = `d.id, d.provider, d.credential_id, c.label, c.username, c.password,
	d.created_at, d.updated_at`

const from = `
	FROM dns_providers d
	JOIN credentials c ON c.id = d.credential_id`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Account, error) {
	var a Account
	if err := row.Scan(&a.ID, &a.Provider, &a.CredentialID, &a.Label,
		&a.Username, &a.Password, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

// Create writes the row and reads it back joined, because an INSERT
// cannot RETURNING across a join and the caller wants the whole thing.
func (r *Repository) Create(ctx context.Context, in Account) (*Account, error) {
	var id int64
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO dns_providers (provider, credential_id) VALUES ($1, $2) RETURNING id`,
		string(in.Provider), in.CredentialID).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create dns provider: %w", err)
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Account, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+from+` WHERE d.id = $1`, id)
	a, err := scan(row)
	if err != nil {
		return nil, database.ErrNotFound
	}
	return a, nil
}

// List is every provider, oldest first — the order they were added is
// the order somebody remembers adding them.
func (r *Repository) List(ctx context.Context) ([]*Account, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+columns+from+` ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Account
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Update re-points a provider at a different stored credential. The
// provider itself is not editable: an account is which API is spoken,
// and changing it in place would be a different account wearing the
// same id.
func (r *Repository) Update(ctx context.Context, id, credentialID int64) (*Account, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE dns_providers SET credential_id = $1, updated_at = now() WHERE id = $2`,
		credentialID, id)
	if err != nil {
		return nil, fmt.Errorf("update dns provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, database.ErrNotFound
	}
	return r.ByID(ctx, id)
}

// UsingCredential are the providers authenticating with one credential
// — what a delete of that credential would break. See
// credential.Dependant.
func (r *Repository) UsingCredential(ctx context.Context, credentialID int64) ([]Provider, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT provider FROM dns_providers WHERE credential_id = $1 ORDER BY provider`, credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Provider
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM dns_providers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete dns provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return database.ErrNotFound
	}
	return nil
}
