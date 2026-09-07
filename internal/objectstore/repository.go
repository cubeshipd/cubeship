package objectstore

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository holds every SQL statement about object stores. Like every
// other repository here it is a thin value over a Queryer, so the same
// code runs on the pool or inside a transaction.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// The join is a LEFT one, because only an external store has a
// credential: a managed store's keys were minted here and live on its
// own row. Which pair a caller gets is decided in scan, so nothing
// above this has to know there are two places a login can come from.
const columns = `s.id, s.slug, s.description, s.kind, s.provider,
	s.endpoint, s.region, s.secure, s.path_style, s.bucket,
	COALESCE(s.credential_id, 0), s.version, s.access_key, s.secret_key,
	s.exposed_port, s.container_id, s.status, s.error,
	COALESCE(c.username, ''), COALESCE(c.password, ''),
	s.created_at, s.updated_at`

const from = `
	FROM object_stores s
	LEFT JOIN credentials c ON c.id = s.credential_id`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Store, error) {
	var s Store
	var ownKey, ownSecret, credUser, credSecret string
	if err := row.Scan(&s.ID, &s.Slug, &s.Description, &s.Kind, &s.Provider,
		&s.Endpoint, &s.Region, &s.Secure, &s.PathStyle, &s.Bucket,
		&s.CredentialID, &s.Version, &ownKey, &ownSecret,
		&s.ExposedPort, &s.ContainerID, &s.Status, &s.Error,
		&credUser, &credSecret,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	if s.Kind == KindManaged {
		s.AccessKey, s.SecretKey = ownKey, ownSecret
	} else {
		s.AccessKey, s.SecretKey = credUser, credSecret
	}
	return &s, nil
}

// Create writes the row and reads it back joined, because an INSERT
// cannot RETURNING across a join and the caller wants the whole thing.
func (r *Repository) Create(ctx context.Context, in *Store) (*Store, error) {
	// A managed store has no credential, and 0 is not a row anything
	// points at — NULL is what the foreign key needs.
	var credentialID any
	if in.CredentialID != 0 {
		credentialID = in.CredentialID
	}
	var id int64
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO object_stores
		   (slug, description, kind, provider, endpoint, region, secure, path_style,
		    bucket, credential_id, version, access_key, secret_key, exposed_port, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 RETURNING id`,
		in.Slug, in.Description, string(in.Kind), string(in.Provider),
		in.Endpoint, in.Region, in.Secure, in.PathStyle, in.Bucket,
		credentialID, in.Version, in.AccessKey, in.SecretKey, in.ExposedPort, in.Status).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.ByID(ctx, id)
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Store, error) {
	s, err := scan(r.q.QueryRowContext(ctx, `SELECT `+columns+from+` WHERE s.id = $1`, id))
	if err != nil {
		return nil, database.ErrNotFound
	}
	return s, nil
}

func (r *Repository) BySlug(ctx context.Context, slug string) (*Store, error) {
	s, err := scan(r.q.QueryRowContext(ctx, `SELECT `+columns+from+` WHERE s.slug = $1`, slug))
	if err != nil {
		return nil, database.ErrNotFound
	}
	return s, nil
}

// List is every store on the instance. Managed ones first, because they
// are the ones with a state worth looking at.
func (r *Repository) List(ctx context.Context) ([]*Store, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+from+` ORDER BY s.kind, s.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Store
	for rows.Next() {
		s, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Update writes whichever field it was given. Both are pointers because
// "leave it alone" and "set it to empty" are different requests.
func (r *Repository) Update(ctx context.Context, id int64, description *string, credentialID *int64) (*Store, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE object_stores
		 SET description   = COALESCE($1, description),
		     credential_id = COALESCE($2, credential_id),
		     updated_at    = now()
		 WHERE id = $3`, description, credentialID, id)
	if err != nil {
		return nil, fmt.Errorf("update object store: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, database.ErrNotFound
	}
	return r.ByID(ctx, id)
}

// UpdateContainer records what a provision did — which container, what
// state, and why not.
func (r *Repository) UpdateContainer(ctx context.Context, id int64, containerID, status, failure string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE object_stores
		 SET container_id = $1, status = $2, error = $3, updated_at = now()
		 WHERE id = $4`, containerID, status, failure, id)
	return err
}

func (r *Repository) SetExposedPort(ctx context.Context, id int64, port int) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE object_stores SET exposed_port = $1, updated_at = now() WHERE id = $2`, port, id)
	return err
}

// UsedPorts are the host ports object stores already answer on. Only
// this module's: a datastore's ports are in another table and the two
// pick from ranges that cannot overlap, which is why neither has to
// read the other's rows.
func (r *Repository) UsedPorts(ctx context.Context) (map[int]bool, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT exposed_port FROM object_stores WHERE exposed_port <> 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	used := map[int]bool{}
	for rows.Next() {
		var port int
		if err := rows.Scan(&port); err != nil {
			return nil, err
		}
		used[port] = true
	}
	return used, rows.Err()
}

// UsingCredential are the stores authenticating with one credential —
// what deleting that credential would break. See credential.Dependant.
func (r *Repository) UsingCredential(ctx context.Context, credentialID int64) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT slug FROM object_stores WHERE credential_id = $1 ORDER BY slug`, credentialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		slugs = append(slugs, slug)
	}
	return slugs, rows.Err()
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM object_stores WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete object store: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete object store: %w", err)
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}
