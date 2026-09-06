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
const columns = `id, project_id, environment_id, slug, description, engine, version,
	username, password, database_name, exposed_port, container_id, status, error, created_at`

type scanner interface{ Scan(dest ...any) error }

// A datastore is almost always read with the slugs it lives under: they
// are what its reference is built from, and a datastore with no
// reference cannot be rendered anywhere. The select list and the source
// are separate constants because one query needs a column added to the
// first — see AttachedTo.
//
// The column order matches scanScoped.
const scopedColumns = `d.id, d.project_id, d.environment_id, d.slug, d.description,
	d.engine, d.version, d.username, d.password, d.database_name, d.exposed_port,
	d.container_id, d.status, d.error, d.created_at, p.slug, e.slug`

const scopedFrom = `
	FROM datastores d
	JOIN projects p ON p.id = d.project_id
	JOIN environments e ON e.id = d.environment_id`

const scopedQuery = `SELECT ` + scopedColumns + scopedFrom

func scanScoped(row scanner) (*Scoped, error) {
	var s Scoped
	if err := row.Scan(&s.ID, &s.ProjectID, &s.EnvironmentID, &s.Slug, &s.Description,
		&s.Engine, &s.Version, &s.Username, &s.Password, &s.Database, &s.ExposedPort,
		&s.ContainerID, &s.Status, &s.Error, &s.CreatedAt,
		&s.ProjectSlug, &s.EnvironmentSlug); err != nil {
		return nil, err
	}
	return &s, nil
}

func scanAll(rows *sql.Rows) ([]*Scoped, error) {
	var out []*Scoped
	for rows.Next() {
		d, err := scanScoped(rows)
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
		 (project_id, environment_id, slug, description, engine, version,
		  username, password, database_name, exposed_port, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+columns,
		d.ProjectID, d.EnvironmentID, d.Slug, d.Description, string(d.Engine), d.Version,
		d.Username, d.Password, d.Database, d.ExposedPort, StatusProvisioning)
	created, err := scanPlain(row)
	if err != nil {
		return nil, fmt.Errorf("create datastore: %w", err)
	}
	return created, nil
}

func scanPlain(row scanner) (*Datastore, error) {
	var d Datastore
	if err := row.Scan(&d.ID, &d.ProjectID, &d.EnvironmentID, &d.Slug, &d.Description,
		&d.Engine, &d.Version, &d.Username, &d.Password, &d.Database, &d.ExposedPort,
		&d.ContainerID, &d.Status, &d.Error, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
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
	d, err := scanPlain(row)
	if err != nil {
		return nil, fmt.Errorf("update datastore: %w", err)
	}
	return d, nil
}

func (r *Repository) ScopedByReference(ctx context.Context, project, env, slug string) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx,
		scopedQuery+` WHERE p.slug = $1 AND e.slug = $2 AND d.slug = $3`, project, env, slug)
	d, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get datastore %s/%s/%s: %w", project, env, slug, err)
	}
	return d, nil
}

func (r *Repository) ScopedByID(ctx context.Context, id int64) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx, scopedQuery+` WHERE d.id = $1`, id)
	d, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get datastore %d: %w", id, err)
	}
	return d, nil
}

func (r *Repository) List(ctx context.Context) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, scopedQuery+` ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func (r *Repository) ListForProject(ctx context.Context, projectID int64) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, scopedQuery+` WHERE d.project_id = $1 ORDER BY d.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAll(rows)
}

func (r *Repository) ListForEnvironment(ctx context.Context, environmentID int64) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, scopedQuery+` WHERE d.environment_id = $1 ORDER BY d.id`, environmentID)
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

// Attachments are the apps wired to one datastore, with the app's own
// name joined in so nothing has to reach into the app module to render
// the list.
func (r *Repository) Attachments(ctx context.Context, datastoreID int64) ([]Attachment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT t.id, t.datastore_id, t.app_id, a.name, t.prefix, t.created_at
		 FROM datastore_attachments t
		 JOIN apps a ON a.id = t.app_id
		 WHERE t.datastore_id = $1 ORDER BY a.name`, datastoreID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.DatastoreID, &a.AppID, &a.AppSlug, &a.Prefix, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Attached is one datastore an app receives variables from, and the
// prefix it receives them under.
type Attached struct {
	Scoped
	Prefix string
}

// AttachedTo is what an app's environment is built from — see
// Service.VarsForApp. Ordered by prefix so a container's environment is
// deterministic whatever order the rows were written in.
func (r *Repository) AttachedTo(ctx context.Context, appID int64) ([]Attached, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+scopedColumns+`, t.prefix`+scopedFrom+`
		 JOIN datastore_attachments t ON t.datastore_id = d.id
		 WHERE t.app_id = $1 ORDER BY t.prefix, d.id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Attached
	for rows.Next() {
		var a Attached
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.EnvironmentID, &a.Slug, &a.Description,
			&a.Engine, &a.Version, &a.Username, &a.Password, &a.Database, &a.ExposedPort,
			&a.ContainerID, &a.Status, &a.Error, &a.CreatedAt,
			&a.ProjectSlug, &a.EnvironmentSlug, &a.Prefix); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) Attach(ctx context.Context, datastoreID, appID int64, prefix string) error {
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO datastore_attachments (datastore_id, app_id, prefix) VALUES ($1, $2, $3)`,
		datastoreID, appID, prefix); err != nil {
		return err
	}
	return nil
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
