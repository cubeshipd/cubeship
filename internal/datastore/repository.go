package datastore

import (
	"context"
	"database/sql"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository is every SQL statement about datastores and what they are
// attached to. It is a thin value over a Queryer, so the same code runs
// on the pool or inside a transaction.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

// columns is read in order by scan. Change one, change both.
//
// A flat list with no joins: a datastore belongs to the instance, so
// there is nothing above it to bring along.
const columns = `id, slug, description, engine, version, username, password,
	database_name, exposed_port, container_id, status, error, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Datastore, error) {
	var d Datastore
	if err := row.Scan(&d.ID, &d.Slug, &d.Description, &d.Engine, &d.Version,
		&d.Username, &d.Password, &d.Database, &d.ExposedPort,
		&d.ContainerID, &d.Status, &d.Error, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func scanAll(rows *sql.Rows) ([]*Datastore, error) {
	var out []*Datastore
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) Create(ctx context.Context, d *Datastore) (*Datastore, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO datastores
		 (slug, description, engine, version, username, password,
		  database_name, exposed_port, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+columns,
		d.Slug, d.Description, string(d.Engine), d.Version,
		d.Username, d.Password, d.Database, d.ExposedPort, StatusProvisioning)
	created, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create datastore: %w", err)
	}
	return created, nil
}

// Update changes a datastore's editable field. A nil argument leaves the
// column alone, so PATCH with nothing named cannot blank it.
//
// There is one, and that is the point: see Service.Update for what the
// others would break.
func (r *Repository) Update(ctx context.Context, id int64, description *string) (*Datastore, error) {
	row := r.q.QueryRowContext(ctx,
		`UPDATE datastores SET description = COALESCE($1, description)
		 WHERE id = $2 RETURNING `+columns, description, id)
	d, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update datastore: %w", err)
	}
	return d, nil
}

func (r *Repository) BySlug(ctx context.Context, slug string) (*Datastore, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM datastores WHERE slug = $1`, slug)
	d, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get datastore %q: %w", slug, err)
	}
	return d, nil
}

func (r *Repository) List(ctx context.Context) ([]*Datastore, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+columns+` FROM datastores ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

// UpdateContainer records which container is serving this datastore and
// how it went. errMsg is written whatever it is, including empty: a
// datastore that has just come up should not still carry why it failed
// the last time.
func (r *Repository) UpdateContainer(ctx context.Context, id int64, containerID, status, errMsg string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE datastores SET container_id = $1, status = $2, error = $3 WHERE id = $4`,
		containerID, status, errMsg, id); err != nil {
		return fmt.Errorf("update datastore container: %w", err)
	}
	return nil
}

// SetExposedPort records the host port this datastore answers on, or 0
// for none. The unique index is what decides a collision, not a
// preceding lookup — two callers exposing at once would both pass one.
func (r *Repository) SetExposedPort(ctx context.Context, id int64, port int) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE datastores SET exposed_port = $1 WHERE id = $2`, port, id); err != nil {
		return fmt.Errorf("set exposed port: %w", err)
	}
	return nil
}

// UsedPorts are the host ports datastores already answer on, which is
// what an automatic allocation skips over.
func (r *Repository) UsedPorts(ctx context.Context) (map[int]bool, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT exposed_port FROM datastores WHERE exposed_port <> 0`)
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

func (r *Repository) Delete(ctx context.Context, id int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM datastores WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete datastore: %w", err)
	}
	return nil
}

// attachmentQuery selects an attachment with the app's full reference
// built in SQL. A datastore is not inside an environment any more, so a
// bare app name identifies nothing — two apps called `api` in two
// projects may both be attached to one database.
const attachmentQuery = `
	SELECT t.id, t.datastore_id, t.app_id,
	       p.slug || '/' || e.slug || '/' || a.name, t.prefix, t.created_at
	FROM datastore_attachments t
	JOIN apps a ON a.id = t.app_id
	JOIN projects p ON p.id = a.project_id
	JOIN environments e ON e.id = a.environment_id`

func scanAttachments(rows *sql.Rows) ([]Attachment, error) {
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.DatastoreID, &a.AppID,
			&a.AppReference, &a.Prefix, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Attachments are the apps wired to one datastore.
func (r *Repository) Attachments(ctx context.Context, datastoreID int64) ([]Attachment, error) {
	rows, err := r.q.QueryContext(ctx,
		attachmentQuery+` WHERE t.datastore_id = $1 ORDER BY 4`, datastoreID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAttachments(rows)
}

// AllAttachments is every attachment on the instance, for a listing
// that would otherwise ask per datastore. One query instead of N.
func (r *Repository) AllAttachments(ctx context.Context) (map[int64][]Attachment, error) {
	rows, err := r.q.QueryContext(ctx, attachmentQuery+` ORDER BY t.datastore_id, 4`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	all, err := scanAttachments(rows)
	if err != nil {
		return nil, err
	}
	byDatastore := map[int64][]Attachment{}
	for _, a := range all {
		byDatastore[a.DatastoreID] = append(byDatastore[a.DatastoreID], a)
	}
	return byDatastore, nil
}

// Attached is one datastore an app receives variables from, and the
// prefix it receives them under.
type Attached struct {
	Datastore
	Prefix string
}

// AttachedTo is what an app's environment is built from — see
// Service.VarsForApp. Ordered by prefix so a container's environment is
// deterministic whatever order the rows were written in.
func (r *Repository) AttachedTo(ctx context.Context, appID int64) ([]Attached, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+`, t.prefix
		 FROM datastores d
		 JOIN datastore_attachments t ON t.datastore_id = d.id
		 WHERE t.app_id = $1 ORDER BY t.prefix, d.id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Attached
	for rows.Next() {
		var a Attached
		if err := rows.Scan(&a.ID, &a.Slug, &a.Description, &a.Engine, &a.Version,
			&a.Username, &a.Password, &a.Database, &a.ExposedPort,
			&a.ContainerID, &a.Status, &a.Error, &a.CreatedAt, &a.Prefix); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) Attach(ctx context.Context, datastoreID, appID int64, prefix string) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO datastore_attachments (datastore_id, app_id, prefix) VALUES ($1, $2, $3)`,
		datastoreID, appID, prefix)
	return err
}

// Detach reports whether it removed anything, so the caller can tell
// "unwired it" from "it was never wired".
func (r *Repository) Detach(ctx context.Context, datastoreID, appID int64) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM datastore_attachments WHERE datastore_id = $1 AND app_id = $2`,
		datastoreID, appID)
	if err != nil {
		return false, fmt.Errorf("detach datastore: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
