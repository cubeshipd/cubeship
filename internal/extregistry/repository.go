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

// A registry row no longer holds a secret: it holds which credential
// authenticates with it. Every read joins that credential, so
// everything above this — the provider clients, the deploy path — still
// sees one value with a provider and a login on it, and none of them
// had to learn where the login now lives.
const columns = `r.id, r.credential_id, c.provider, r.host, r.namespace, r.region,
	c.username, c.password, r.created_at, r.updated_at`

const from = `
	FROM external_registries r
	JOIN credentials c ON c.id = r.credential_id`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Credential, error) {
	var c Credential
	if err := row.Scan(&c.ID, &c.CredentialID, &c.Provider, &c.Host, &c.Namespace, &c.Region,
		&c.Username, &c.Password, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	// DigitalOcean's registry takes the API token as both halves of a
	// docker login, and a DigitalOcean credential stores one value:
	// there is no name beside a token, so the account has no username
	// to give. It is doubled here, on the way out, because every caller
	// below wants a usable login and none of them should have to know
	// this — a pull with an empty username is refused by the registry
	// and reads as a wrong token.
	if c.Provider == ProviderDigitalOcean && c.Username == "" {
		c.Username = c.Password
	}
	return &c, nil
}

// Create writes the row and reads it back joined, because an INSERT
// cannot RETURNING across a join and the caller wants the whole thing.
func (r *Repository) Create(ctx context.Context, in Credential) (*Credential, error) {
	var id int64
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO external_registries (credential_id, host, namespace, region)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		in.CredentialID, in.Host, in.Namespace, in.Region).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create registry: %w", err)
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Credential, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+from+` WHERE r.id = $1`, id)
	c, err := scan(row)
	if err != nil {
		return nil, database.ErrNotFound
	}
	return c, nil
}

// UsingCredential are the hosts authenticating with one credential —
// what a delete of that credential would break. See credential.Dependant.
func (r *Repository) UsingCredential(ctx context.Context, credentialID int64) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT host FROM external_registries WHERE credential_id = $1 ORDER BY host`, credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hosts []string
	for rows.Next() {
		var host string
		if err := rows.Scan(&host); err != nil {
			return nil, err
		}
		hosts = append(hosts, host)
	}
	return hosts, rows.Err()
}

// Update writes whichever field it was given.
//
// Both are pointers because "leave it alone" and "set it to empty" are
// different requests, and nil is the only way to say the first.
//
// The login is not among them any more. Rotating a secret is an edit to
// the credential, in one place, and every registry using it follows —
// which is the whole reason credentials were pulled out of here.
func (r *Repository) Update(ctx context.Context, id int64, credentialID *int64, namespace *string) (*Credential, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE external_registries
		 SET credential_id = COALESCE($1, credential_id),
		     namespace     = COALESCE($2, namespace),
		     updated_at    = now()
		 WHERE id = $3`, credentialID, namespace, id)
	if err != nil {
		return nil, fmt.Errorf("update registry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, database.ErrNotFound
	}
	return r.ByID(ctx, id)
}

func (r *Repository) List(ctx context.Context) ([]*Credential, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+columns+from+` ORDER BY r.host`)
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
// answers whether the instance has a way in. A miss is not an error — a
// public image needs no credential.
func (r *Repository) ByHost(ctx context.Context, host string) (*Credential, bool, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+from+` WHERE r.host = $1`, host)
	c, err := scan(row)
	if err != nil {
		return nil, false, nil
	}
	return c, true, nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM external_registries WHERE id = $1`, id)
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
