package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const columns = `id, project_id, environment_id, name, description, source, source_image,
	source_repo, source_ref, source_dockerfile, container_id, status, env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*App, error) {
	var a App
	var envJSON []byte
	if err := row.Scan(&a.ID, &a.ProjectID, &a.EnvironmentID, &a.Name, &a.Description,
		&a.Source, &a.SourceImage, &a.SourceRepo, &a.SourceRef, &a.SourceDockerfile,
		&a.ContainerID, &a.Status, &envJSON, &a.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	return &a, nil
}

// Update changes an app's configuration: what it is, where it is
// served, and where its image comes from. A nil field is left alone, so
// saving one section of the settings screen cannot blank another.
//
// The slug is not here. It is the last component of the app's registry
// reference, and no slug in Cubeship changes once its resource exists.
func (r *Repository) Update(ctx context.Context, appID int64, description *string, source *Source, origin *Origin) (*App, error) {
	var src *string
	if source != nil {
		s := string(*source)
		src = &s
	}
	// The origin fields travel with the source: changing one without
	// the other would leave an app naming an image its source ignores.
	// Passing them as one nil means "leave all four".
	var image, repo, ref, dockerfile *string
	if origin != nil {
		image, repo, ref, dockerfile = &origin.Image, &origin.Repo, &origin.Ref, &origin.Dockerfile
	}
	row := r.q.QueryRowContext(ctx,
		`UPDATE apps SET
		   description       = COALESCE($1, description),
		   source            = COALESCE($2, source),
		   source_image      = COALESCE($3, source_image),
		   source_repo       = COALESCE($4, source_repo),
		   source_ref        = COALESCE($5, source_ref),
		   source_dockerfile = COALESCE($6, source_dockerfile)
		 WHERE id = $7 RETURNING `+columns,
		description, src, image, repo, ref, dockerfile, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update app: %w", err)
	}
	return a, nil
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

func (r *Repository) Create(ctx context.Context, projectID, environmentID int64, name, description string, source Source, origin Origin) (*App, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO apps (project_id, environment_id, name, description, source,
		                   source_image, source_repo, source_ref, source_dockerfile)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+columns,
		projectID, environmentID, name, description, string(source),
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

// List returns every app on the instance.
func (r *Repository) List(ctx context.Context) ([]*App, error) {
	return r.list(ctx, `SELECT `+columns+` FROM apps ORDER BY id`)
}

// ListForProject and ListForEnvironment are what deleting something
// above an app reads: everything that has to be stopped before the row
// above it can go. See project.AppTeardown.
func (r *Repository) ListForProject(ctx context.Context, projectID int64) ([]*App, error) {
	return r.list(ctx, `SELECT `+columns+` FROM apps WHERE project_id = $1 ORDER BY id`, projectID)
}

func (r *Repository) ListForEnvironment(ctx context.Context, environmentID int64) ([]*App, error) {
	return r.list(ctx, `SELECT `+columns+` FROM apps WHERE environment_id = $1 ORDER BY id`, environmentID)
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
	ProjectSlug     string
	EnvironmentSlug string
}

// scopedQuery selects an app with its containing slugs. The column order
// matches scanScoped.
const scopedQuery = `
	SELECT a.id, a.project_id, a.environment_id, a.name, a.description,
	       a.source, a.source_image, a.source_repo, a.source_ref, a.source_dockerfile,
	       a.container_id, a.status, a.env, a.created_at,
	       p.slug, e.slug
	FROM apps a
	JOIN projects p ON p.id = a.project_id
	JOIN environments e ON e.id = a.environment_id`

func scanScoped(row scanner) (*Scoped, error) {
	var s Scoped
	var envJSON []byte
	if err := row.Scan(&s.ID, &s.ProjectID, &s.EnvironmentID, &s.Name, &s.Description,
		&s.Source, &s.SourceImage, &s.SourceRepo, &s.SourceRef, &s.SourceDockerfile,
		&s.ContainerID, &s.Status, &envJSON, &s.CreatedAt,
		&s.ProjectSlug, &s.EnvironmentSlug); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &s.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", s.Name, err)
	}
	return &s, nil
}

// BuildingFromRepository finds every app that builds from a repository
// at a branch.
//
// The repository is matched on the "owner/name" a URL and a webhook
// payload both reduce to, because the two are rarely spelled the same —
// one may carry .git, a trailing slash, or www.
//
// An app with no ref of its own tracks whatever branch it is told about,
// which is what makes "deploy on push" work without anybody naming a
// branch twice.
func (r *Repository) BuildingFromRepository(ctx context.Context, fullName, branch string) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, scopedQuery+`
		WHERE a.source = ANY($1)
		  AND lower(regexp_replace(regexp_replace(a.source_repo, '^https?://(www\.)?github\.com/', ''), '(\.git)?/?$', '')) = lower($2)
		  AND (a.source_ref = '' OR a.source_ref = $3)`,
		[]string{string(SourceDockerfile), string(SourceRailpack)}, fullName, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Scoped
	for rows.Next() {
		a, err := scanScoped(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ScopedByReference resolves the three-part reference that identifies an
// app, in one query.
func (r *Repository) ScopedByReference(ctx context.Context, proj, env, name string) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx,
		scopedQuery+` WHERE p.slug = $1 AND e.slug = $2 AND a.name = $3`,
		proj, env, name)
	s, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get app %s/%s/%s: %w", proj, env, name, err)
	}
	// Loaded here rather than by whoever asked. An app with no domains
	// cannot deploy at all, so every reader of a single app needs them —
	// and one that forgot would get an app that looks unroutable.
	if s.Domains, err = r.Domains(ctx, s.ID); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *Repository) ScopedByID(ctx context.Context, id int64) (*Scoped, error) {
	row := r.q.QueryRowContext(ctx, scopedQuery+` WHERE a.id = $1`, id)
	s, err := scanScoped(row)
	if err != nil {
		return nil, fmt.Errorf("get app %d: %w", id, err)
	}
	if s.Domains, err = r.Domains(ctx, s.ID); err != nil {
		return nil, err
	}
	return s, nil
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

// Domains reads every name an app is served at.
//
// A separate read rather than a join: an app's row is fetched in
// listings where the domains are not looked at, and a join would turn
// one row per app into one per domain for every one of them.
func (r *Repository) Domains(ctx context.Context, appID int64) ([]Domain, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id, host, port FROM app_domains WHERE app_id = $1 ORDER BY id`, appID)
	if err != nil {
		return nil, fmt.Errorf("list app domains: %w", err)
	}
	defer rows.Close()

	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Host, &d.Port); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DomainsFor reads the domains of several apps at once, keyed by app.
//
// A listing shows what each app answers at, and asking per app would be
// one query per row.
func (r *Repository) DomainsFor(ctx context.Context, appIDs []int64) (map[int64][]Domain, error) {
	out := map[int64][]Domain{}
	if len(appIDs) == 0 {
		return out, nil
	}
	// Placeholders rather than ANY($1): passing a slice through
	// database/sql depends on the driver marshalling it, and the rest of
	// this package never asks that of it.
	holders := make([]string, len(appIDs))
	args := make([]any, len(appIDs))
	for i, id := range appIDs {
		holders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT app_id, id, host, port FROM app_domains
		 WHERE app_id IN (`+strings.Join(holders, ", ")+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list app domains: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var appID int64
		var d Domain
		if err := rows.Scan(&appID, &d.ID, &d.Host, &d.Port); err != nil {
			return nil, err
		}
		out[appID] = append(out[appID], d)
	}
	return out, rows.Err()
}

// AddDomain gives an app a name to answer at.
func (r *Repository) AddDomain(ctx context.Context, appID int64, host string, port int) (*Domain, error) {
	var d Domain
	err := r.q.QueryRowContext(ctx,
		`INSERT INTO app_domains (app_id, host, port) VALUES ($1, $2, $3) RETURNING id, host, port`,
		appID, host, port).Scan(&d.ID, &d.Host, &d.Port)
	if err != nil {
		return nil, fmt.Errorf("add app domain: %w", err)
	}
	return &d, nil
}

// SetDomainPort changes what one name reaches.
func (r *Repository) SetDomainPort(ctx context.Context, appID, domainID int64, port int) error {
	result, err := r.q.ExecContext(ctx,
		`UPDATE app_domains SET port = $1 WHERE id = $2 AND app_id = $3`, port, domainID, appID)
	if err != nil {
		return fmt.Errorf("set app domain port: %w", err)
	}
	return affected(result)
}

// RemoveDomain takes a name off an app.
func (r *Repository) RemoveDomain(ctx context.Context, appID, domainID int64) error {
	result, err := r.q.ExecContext(ctx,
		`DELETE FROM app_domains WHERE id = $1 AND app_id = $2`, domainID, appID)
	if err != nil {
		return fmt.Errorf("remove app domain: %w", err)
	}
	return affected(result)
}

func affected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return database.ErrNotFound
	}
	return nil
}
