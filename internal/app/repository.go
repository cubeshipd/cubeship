package app

import (
	"context"
	"fmt"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const columns = `id, org_id, project_id, environment_id, name, domain, source, source_image,
	source_repo, source_ref, source_dockerfile, container_id, status, env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*App, error) {
	var a App
	var envJSON []byte
	if err := row.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.EnvironmentID, &a.Name, &a.Domain,
		&a.Source, &a.SourceImage, &a.SourceRepo, &a.SourceRef, &a.SourceDockerfile,
		&a.ContainerID, &a.Status, &envJSON, &a.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	return &a, nil
}

// Origin is where an app's images come from, beyond the source that
// says which of these fields mean anything. Passing them as one value
// keeps Create from growing an argument per source.
type Origin struct {
	Image      string
	Repo       string
	Ref        string
	Dockerfile string
}

func (r *Repository) Create(ctx context.Context, orgID, projectID, environmentID int64, name, domain string, source Source, origin Origin) (*App, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO apps (org_id, project_id, environment_id, name, domain, source,
		                   source_image, source_repo, source_ref, source_dockerfile)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+columns,
		orgID, projectID, environmentID, name, domain, string(source),
		origin.Image, origin.Repo, origin.Ref, origin.Dockerfile)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return a, nil
}

// ByEnvironmentAndName is the only way to look one app up by name: a
// name is unique within its environment and nowhere wider.
func (r *Repository) ByEnvironmentAndName(ctx context.Context, environmentID int64, name string) (*App, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM apps WHERE environment_id = $1 AND name = $2`, environmentID, name)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get app %q: %w", name, err)
	}
	return a, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*App, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM apps WHERE id = $1`, id)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get app %d: %w", id, err)
	}
	return a, nil
}

// Delete removes an app and the deployment history that points at it.
// The caller is responsible for the container first — a row deleted
// while its container runs leaves something serving traffic that nothing
// knows how to stop.
func (r *Repository) Delete(ctx context.Context, appID int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM deployments WHERE app_id = $1`, appID); err != nil {
		return fmt.Errorf("delete deployments: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM apps WHERE id = $1`, appID); err != nil {
		return fmt.Errorf("delete app: %w", err)
	}
	return nil
}

// List returns every app on the instance. Only the reconciler, which is
// instance-wide by nature, should use it; callers acting for a user want
// ListScopedForOrgs so the database does the filtering.
func (r *Repository) List(ctx context.Context) ([]*App, error) {
	return r.list(ctx, `SELECT `+columns+` FROM apps ORDER BY id`)
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*App, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []*App
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (r *Repository) UpdateContainer(ctx context.Context, appID int64, containerID, status string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE apps SET container_id = $1, status = $2 WHERE id = $3`,
		containerID, status, appID); err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	return nil
}

// SetEnv replaces the app's variables wholesale. Callers that mean "add
// these" want MergeEnv — this one deletes every key not in env.
func (r *Repository) SetEnv(ctx context.Context, appID int64, env envvar.Map) error {
	envJSON, err := envvar.MarshalJSONB(env)
	if err != nil {
		return err
	}
	if _, err := r.q.ExecContext(ctx,
		`UPDATE apps SET env = $1::jsonb WHERE id = $2`, envJSON, appID); err != nil {
		return fmt.Errorf("set app env: %w", err)
	}
	return nil
}

// MergeEnv sets the given variables and removes the unset ones, leaving
// every other key alone. This is what "env set" means to a user: add
// these, keep the rest.
func (r *Repository) MergeEnv(ctx context.Context, appID int64, set envvar.Map, unset []string) error {
	setJSON, err := envvar.MarshalJSONB(set)
	if err != nil {
		return err
	}
	return database.MergeJSONBMap(ctx, r.q, "apps", "env", appID, setJSON, unset)
}

const deploymentColumns = `id, app_id, image_ref, status, error, logs, created_at`

func scanDeployment(row scanner) (*Deployment, error) {
	var d Deployment
	if err := row.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.Status, &d.Error, &d.Logs, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// StartDeployment records a deploy that is about to begin. The row is
// what a caller polls afterwards, so it has to exist before any work
// does — including before the response that hands back its id.
//
// imageRef is what was asked for, which for a source that builds is not
// yet an image at all; SetDeploymentImage fills in what actually ran.
func (r *Repository) StartDeployment(ctx context.Context, appID int64, imageRef string) (*Deployment, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO deployments (app_id, image_ref, status) VALUES ($1, $2, $3) RETURNING `+deploymentColumns,
		appID, imageRef, DeploymentPending)
	d, err := scanDeployment(row)
	if err != nil {
		return nil, fmt.Errorf("start deployment: %w", err)
	}
	return d, nil
}

// SetDeploymentImage records the image a deploy resolved to, once the
// source has produced one.
func (r *Repository) SetDeploymentImage(ctx context.Context, id int64, imageRef string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE deployments SET image_ref = $1 WHERE id = $2`, imageRef, id); err != nil {
		return fmt.Errorf("record deployment image: %w", err)
	}
	return nil
}

// FinishDeployment writes a deploy's outcome.
func (r *Repository) FinishDeployment(ctx context.Context, id int64, status, errMsg string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE deployments SET status = $1, error = $2 WHERE id = $3`, status, errMsg, id); err != nil {
		return fmt.Errorf("finish deployment: %w", err)
	}
	return nil
}

// DeploymentByID reads one deployment, scoped to its app so an id from
// another app's history resolves to nothing.
// SetDeploymentLogs replaces a deployment's captured output. It is
// called repeatedly while a build runs, so it replaces rather than
// appends: the writer holds the whole text and the row is a mirror of
// it, which cannot drift or interleave with a concurrent write.
func (r *Repository) SetDeploymentLogs(ctx context.Context, id int64, logs string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE deployments SET logs = $1 WHERE id = $2`, logs, id); err != nil {
		return fmt.Errorf("save deployment logs: %w", err)
	}
	return nil
}

func (r *Repository) DeploymentByID(ctx context.Context, appID, id int64) (*Deployment, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+deploymentColumns+` FROM deployments WHERE id = $1 AND app_id = $2`, id, appID)
	d, err := scanDeployment(row)
	if err != nil {
		return nil, fmt.Errorf("get deployment %d: %w", id, err)
	}
	return d, nil
}

// ListDeployments returns an app's deploy history, newest first.
func (r *Repository) ListDeployments(ctx context.Context, appID int64, limit int) ([]*Deployment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+deploymentColumns+` FROM deployments WHERE app_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`,
		appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Scoped is an app together with the slugs of everything that contains
// it. Reading those in one join is what keeps listing apps a single
// query instead of three more per app.
type Scoped struct {
	App
	OrgSlug         string
	ProjectSlug     string
	EnvironmentSlug string
}

// scopedQuery selects an app with its containing slugs. The column order
// matches scanScoped.
const scopedQuery = `
	SELECT a.id, a.org_id, a.project_id, a.environment_id, a.name, a.domain,
	       a.source, a.source_image, a.source_repo, a.source_ref, a.source_dockerfile,
	       a.container_id, a.status, a.env, a.created_at,
	       o.slug, p.slug, e.slug
	FROM apps a
	JOIN organizations o ON o.id = a.org_id
	JOIN projects p ON p.id = a.project_id
	JOIN environments e ON e.id = a.environment_id`

func scanScoped(row scanner) (*Scoped, error) {
	var s Scoped
	var envJSON []byte
	if err := row.Scan(&s.ID, &s.OrgID, &s.ProjectID, &s.EnvironmentID, &s.Name, &s.Domain,
		&s.Source, &s.SourceImage, &s.SourceRepo, &s.SourceRef, &s.SourceDockerfile,
		&s.ContainerID, &s.Status, &envJSON, &s.CreatedAt,
		&s.OrgSlug, &s.ProjectSlug, &s.EnvironmentSlug); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &s.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", s.Name, err)
	}
	return &s, nil
}

// ScopedByReference resolves the four-part reference that identifies an
// app, in one query.
func (r *Repository) ScopedByReference(ctx context.Context, org, proj, env, name string) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx,
		scopedQuery+` WHERE o.slug = $1 AND p.slug = $2 AND e.slug = $3 AND a.name = $4`,
		org, proj, env, name)
	s, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get app %s/%s/%s/%s: %w", org, proj, env, name, err)
	}
	return s, nil
}

func (r *Repository) ScopedByID(ctx context.Context, id int64) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx, scopedQuery+` WHERE a.id = $1`, id)
	s, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get app %d: %w", id, err)
	}
	return s, nil
}

// CountInProject reports how many apps live anywhere in a project.
// Deleting a project is refused while any remain.
func (r *Repository) CountInProject(ctx context.Context, projectID int64) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM apps WHERE project_id = $1`, projectID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count apps in project: %w", err)
	}
	return n, nil
}

// ListScopedForOrgs returns the apps owned by any of orgIDs, each with
// its containing slugs. An empty orgIDs returns nothing rather than
// everything — a caller who belongs to no organization sees no apps.
func (r *Repository) ListScopedForOrgs(ctx context.Context, orgIDs []int64) ([]*Scoped, error) {
	if len(orgIDs) == 0 {
		return nil, nil
	}
	return r.listScoped(ctx, scopedQuery+` WHERE a.org_id = ANY($1) ORDER BY a.id`, orgIDs)
}

// ListScoped returns every app on the instance with its containing
// slugs — what a super-admin sees.
func (r *Repository) ListScoped(ctx context.Context) ([]*Scoped, error) {
	return r.listScoped(ctx, scopedQuery+` ORDER BY a.id`)
}

func (r *Repository) listScoped(ctx context.Context, query string, args ...any) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Scoped
	for rows.Next() {
		s, err := scanScoped(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
