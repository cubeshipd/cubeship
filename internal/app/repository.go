package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
	source_tag, source_repo, source_ref, source_dockerfile, health_path, scale, spread,
	cpu_limit, memory_limit,
	autoscale_min, autoscale_max, autoscale_cpu, autoscaled_at,
	env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*App, error) {
	var a App
	var envJSON []byte
	if err := row.Scan(&a.ID, &a.ProjectID, &a.EnvironmentID, &a.Name, &a.Description,
		&a.Source, &a.SourceImage, &a.SourceTag, &a.SourceRepo, &a.SourceRef, &a.SourceDockerfile,
		&a.HealthPath, &a.Scale, &a.Spread,
		&a.Limits.CPU, &a.Limits.Memory,
		&a.Autoscale.Min, &a.Autoscale.Max, &a.Autoscale.CPU, &a.Autoscale.At,
		&envJSON, &a.CreatedAt); err != nil {
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
func (r *Repository) Update(ctx context.Context, appID int64, description *string, source *Source, origin *Origin, health *string, limits *Limits, auto *Autoscale) (*App, error) {
	var src *string
	if source != nil {
		s := string(*source)
		src = &s
	}
	// The origin fields travel with the source: changing one without
	// the other would leave an app naming an image its source ignores.
	// Passing them as one nil means "leave all four".
	var image, tag, repo, ref, dockerfile *string
	if origin != nil {
		image, tag = &origin.Image, &origin.Tag
		repo, ref, dockerfile = &origin.Repo, &origin.Ref, &origin.Dockerfile
	}
	// Both halves of the ceiling travel together, and zero is a value
	// rather than a gap: it is how a limit is *removed*, so a nil here
	// has to be the only way of saying "leave it".
	var cpu *float64
	var memory *int64
	if limits != nil {
		cpu, memory = &limits.CPU, &limits.Memory
	}
	// The rule travels whole for the same reason: zero is how it is
	// turned off, so a nil is the only way of saying "leave it".
	// `autoscaled_at` is not here — it belongs to the rule acting, not
	// to somebody editing it, and clearing it on an edit would hand out
	// a free pass through the cooldown.
	var autoMin, autoMax *int
	var autoCPU *float64
	if auto != nil {
		autoMin, autoMax, autoCPU = &auto.Min, &auto.Max, &auto.CPU
	}
	row := r.q.QueryRowContext(ctx,
		`UPDATE apps SET
		   description       = COALESCE($1, description),
		   source            = COALESCE($2, source),
		   source_image      = COALESCE($3, source_image),
		   source_tag        = COALESCE($4, source_tag),
		   source_repo       = COALESCE($5, source_repo),
		   source_ref        = COALESCE($6, source_ref),
		   source_dockerfile = COALESCE($7, source_dockerfile),
		   health_path       = COALESCE($8, health_path),
		   cpu_limit         = COALESCE($9, cpu_limit),
		   memory_limit      = COALESCE($10, memory_limit),
		   autoscale_min     = COALESCE($11, autoscale_min),
		   autoscale_max     = COALESCE($12, autoscale_max),
		   autoscale_cpu     = COALESCE($13, autoscale_cpu)
		 WHERE id = $14 RETURNING `+columns,
		description, src, image, tag, repo, ref, dockerfile, health, cpu, memory,
		autoMin, autoMax, autoCPU, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update app: %w", err)
	}
	return a, r.attach(ctx, []*App{a})
}

// Origin is where an app's images come from, beyond the source that
// says which of these fields mean anything. Passing them as one value
// keeps Create from growing an argument per source.
type Origin struct {
	Image string
	// Tag is the one this app runs, and empty is a decision rather than
	// a gap: on Cubeship's own registry it means "whatever is pushed",
	// which is the push-deploys-it behaviour every app has had; on any
	// other registry it means `latest`.
	//
	// It is the whole of the autodeploy switch. A tag beside a flag
	// saying to follow every push is two settings that can contradict
	// each other, and one of them would have to lose silently.
	Tag        string
	Repo       string
	Ref        string
	Dockerfile string
}

func (r *Repository) Create(ctx context.Context, projectID, environmentID int64, name, description string, source Source, origin Origin) (*App, error) {
	row := r.q.QueryRowContext(ctx,
		// An app is created on the machine the daemon is on, and moved
		// afterwards if it belongs somewhere else. The subquery rather
		// than a column default because a default would have to name a
		// row by a number, and the control plane's is a fact about a
		// table rather than a constant.
		`INSERT INTO apps (project_id, environment_id, name, description, source,
		                   source_image, source_tag, source_repo, source_ref, source_dockerfile)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+columns,
		projectID, environmentID, name, description, string(source),
		origin.Image, origin.Tag, origin.Repo, origin.Ref, origin.Dockerfile)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	// And it runs there too. The two are separate facts — where an app
	// is served and where it runs — and on a fresh app they are the
	// same machine, because there is only one until somebody adds
	// another.
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO app_nodes (app_id, node_id, ordinal)
		 VALUES ($1, (SELECT id FROM nodes WHERE control_plane), 1)`, a.ID); err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	a.Replicas, err = r.Replicas(ctx, a.ID)
	if err != nil {
		return nil, err
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
	return a, r.attach(ctx, []*App{a})
}

func (r *Repository) ByID(ctx context.Context, id int64) (*App, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM apps WHERE id = $1`, id)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get app %d: %w", id, err)
	}
	return a, r.attach(ctx, []*App{a})
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return apps, r.attach(ctx, apps)
}

// attach fills in every app's replicas, because an app read without
// them has no status at all — App.Status is derived from them, so a
// caller that forgot would see every app as pending. Loaded here rather
// than by whoever asked, which is the rule Domains already follows for
// the same reason.
func (r *Repository) attach(ctx context.Context, apps []*App) error {
	if len(apps) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	byApp, err := r.ReplicasFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, a := range apps {
		a.Replicas = byApp[a.ID]
	}
	return nil
}

// UpdateContainer records what is running an app on one machine.
//
// An upsert rather than an update: a machine reporting a container for
// an app it was given but has never run has no row to update yet, and
// the report is exactly the moment the row becomes true.
func (r *Repository) UpdateContainer(ctx context.Context, appID, nodeID int64, ordinal int, containerID, name string, deployment int64, status string) error {
	var deploy any
	if deployment != 0 {
		deploy = deployment
	}
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO app_nodes (app_id, node_id, ordinal, container_id, container_name, deployment_id, status, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (app_id, node_id, ordinal) DO UPDATE
		 SET container_id = EXCLUDED.container_id, container_name = EXCLUDED.container_name,
		     deployment_id = EXCLUDED.deployment_id,
		     status = EXCLUDED.status, updated_at = now()`,
		appID, nodeID, ordinal, containerID, name, deploy, status); err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	return nil
}

// SetStatus changes what one machine says about an app without touching
// which container it named. It is what the reconciler writes: it looked
// at the container that is already recorded and found it stopped.
func (r *Repository) SetStatus(ctx context.Context, appID, nodeID int64, ordinal int, status string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE app_nodes SET status = $4, updated_at = now()
		 WHERE app_id = $1 AND node_id = $2 AND ordinal = $3`,
		appID, nodeID, ordinal, status); err != nil {
		return fmt.Errorf("set app status: %w", err)
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

// deploymentListColumns is the same minus the log itself, which is up
// to MaxDeploymentLogBytes a row against fifty rows of history — and
// which the dashboard would then re-fetch every two seconds while a
// build is running. Whether there *is* one is all a listing needs.
const deploymentListColumns = `id, app_id, image_ref, status, error, logs <> '' AS has_logs, created_at`

func scanDeployment(row scanner) (*Deployment, error) {
	var d Deployment
	if err := row.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.Status, &d.Error, &d.Logs, &d.CreatedAt); err != nil {
		return nil, err
	}
	// Derived rather than selected twice: a read that carries the log
	// answers the question by holding one.
	d.HasLogs = d.Logs != ""
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

// UnscopedDeployment reads one deploy by id alone.
//
// Every other read of a deployment is scoped to its app, so an id from
// another app's history resolves to nothing. This one is not, and the
// reason is that its caller does not have an app: it is a machine
// reporting on a deploy this instance handed it. What stands in for the
// scope is the machine's own credential, plus the check that the app is
// still placed there — see Service.Placed.
func (r *Repository) UnscopedDeployment(ctx context.Context, id int64) (*Deployment, error) {
	d, err := scanDeployment(r.q.QueryRowContext(ctx,
		`SELECT `+deploymentColumns+` FROM deployments WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get deployment %d: %w", id, err)
	}
	return d, nil
}

// DeleteDeployment removes one deploy's record, and reports whether
// there was one to remove.
//
// The record, and nothing else. The container this app is running is
// the app's, not the deployment's, and the image is in a registry that
// needs a garbage collection pass Cubeship does not run — the same
// thing that is true of an app's images when the app itself goes. What
// this reclaims is the build log, which is most of the row.
func (r *Repository) DeleteDeployment(ctx context.Context, appID, id int64) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`DELETE FROM deployments WHERE id = $1 AND app_id = $2`, id, appID)
	if err != nil {
		return false, fmt.Errorf("delete deployment %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// CurrentDeployment is the deploy whose image the app is running: the
// newest one that succeeded.
//
// Derived rather than stored, because it already is. The orchestrator
// swaps the container in and *then* finishes the deployment as
// succeeded, so the newest succeeded row is by construction the one
// behind what is running. A column saying so would be a second copy of
// that fact, kept in step by hand.
func (r *Repository) CurrentDeployment(ctx context.Context, appID int64) (int64, bool, error) {
	var id int64
	err := r.q.QueryRowContext(ctx,
		`SELECT id FROM deployments
		 WHERE app_id = $1 AND status = $2
		 ORDER BY created_at DESC, id DESC LIMIT 1`, appID, DeploymentSucceeded).Scan(&id)
	if err != nil {
		return 0, false, nil
	}
	return id, true, nil
}

// DeploymentToRun is the one a machine should be running for an app:
// the newest that resolved to an image and did not fail.
//
// Not simply the newest. A deploy that failed on the machine is one the
// machine should stop trying — and the row under it, the last one that
// worked, is what it should be running instead. That is a rollback, and
// it falls out of asking the question this way rather than being a path
// somebody has to write.
func (r *Repository) DeploymentToRun(ctx context.Context, appID int64) (*Deployment, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+deploymentListColumns+`
		 FROM deployments
		 WHERE app_id = $1 AND image_ref <> '' AND status <> $2
		 ORDER BY created_at DESC, id DESC LIMIT 1`, appID, DeploymentFailed)
	d, err := scanDeploymentSummary(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the deployment to run: %w", err)
	}
	return d, nil
}

// scanDeploymentSummary reads a row selected with
// deploymentListColumns: everything about the deploy except what it
// printed.
func scanDeploymentSummary(row scanner) (*Deployment, error) {
	var d Deployment
	if err := row.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.Status, &d.Error, &d.HasLogs, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// OpenDeployment is the app's newest deploy that has not finished, or
// nil when every one of them has.
//
// There is at most one worth caring about: deploys of one app are
// serialized, so an unfinished row below a finished one is a row
// nothing is coming for.
func (r *Repository) OpenDeployment(ctx context.Context, appID int64) (*Deployment, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+deploymentColumns+` FROM deployments
		 WHERE app_id = $1 AND status = $2 ORDER BY id DESC LIMIT 1`,
		appID, DeploymentPending)
	d, err := scanDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find the open deployment of app %d: %w", appID, err)
	}
	return d, nil
}

// PendingSince is every unfinished deploy on the instance older than
// cutoff, newest first. What the sweeper reads — see Service.Sweep.
func (r *Repository) PendingSince(ctx context.Context, cutoff time.Time) ([]*Deployment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+deploymentColumns+` FROM deployments
		 WHERE status = $1 AND created_at < $2 ORDER BY id DESC`,
		DeploymentPending, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list unfinished deploys: %w", err)
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

// ListDeployments returns an app's deploy history, newest first, with
// no logs on it — see deploymentListColumns.
func (r *Repository) ListDeployments(ctx context.Context, appID int64, limit int) ([]*Deployment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+deploymentListColumns+` FROM deployments WHERE app_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`,
		appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Deployment
	for rows.Next() {
		d, err := scanDeploymentSummary(rows)
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
	       a.health_path, a.scale, a.spread, a.cpu_limit, a.memory_limit,
	       a.autoscale_min, a.autoscale_max, a.autoscale_cpu, a.autoscaled_at,
	       a.env, a.created_at,
	       p.slug, e.slug
	FROM apps a
	JOIN projects p ON p.id = a.project_id
	JOIN environments e ON e.id = a.environment_id`

func scanScoped(row scanner) (*Scoped, error) {
	var s Scoped
	var envJSON []byte
	if err := row.Scan(&s.ID, &s.ProjectID, &s.EnvironmentID, &s.Name, &s.Description,
		&s.Source, &s.SourceImage, &s.SourceRepo, &s.SourceRef, &s.SourceDockerfile,
		&s.HealthPath, &s.Scale, &s.Spread,
		&s.Limits.CPU, &s.Limits.Memory,
		&s.Autoscale.Min, &s.Autoscale.Max, &s.Autoscale.CPU, &s.Autoscale.At,
		&envJSON, &s.CreatedAt,
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
	// and one that forgot would get an app that looks unroutable. Its
	// replicas are the same rule and a stronger one: the status is
	// derived from them, so a reader that forgot would see every app as
	// pending.
	if s.Domains, err = r.Domains(ctx, s.ID); err != nil {
		return nil, err
	}
	return s, r.attach(ctx, []*App{&s.App})
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
	return s, r.attach(ctx, []*App{&s.App})
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

// ScopedOnNode is every app that runs on one machine, with what it
// needs to be run there.
//
// Its replica set rather than its edge: a machine runs the apps it was
// given, and the machine serving an app's names is a different question
// — one that machine answers with a router rather than a container.
// Ordered by id so a node's desired state is stable between passes
// rather than reshuffling under whatever reads it.
func (r *Repository) ScopedOnNode(ctx context.Context, nodeID int64) ([]*Scoped, error) {
	return r.listScoped(ctx, scopedQuery+`
		WHERE EXISTS (SELECT 1 FROM app_nodes r WHERE r.app_id = a.id AND r.node_id = $1)
		ORDER BY a.id`, nodeID)
}

// Replicas reads the machines one app runs on.
//
// Ordered by node id, so the list is stable between two reads of the
// same app — a listing that reshuffled would make the edge's load
// balancer look like it had changed when nothing had.
func (r *Repository) Replicas(ctx context.Context, appID int64) ([]Replica, error) {
	byApp, err := r.ReplicasFor(ctx, []int64{appID})
	if err != nil {
		return nil, err
	}
	return byApp[appID], nil
}

// ReplicasFor reads the replicas of several apps at once, keyed by app.
// A listing needs every app's machines and asking per app would be one
// query per row — the same argument DomainsFor makes.
func (r *Repository) ReplicasFor(ctx context.Context, appIDs []int64) (map[int64][]Replica, error) {
	out := map[int64][]Replica{}
	if len(appIDs) == 0 {
		return out, nil
	}
	rows, err := r.q.QueryContext(ctx, `
		SELECT r.app_id, r.node_id, n.slug, r.ordinal, r.container_id, r.container_name,
		       COALESCE(r.deployment_id, 0), r.status, r.updated_at
		FROM app_nodes r
		JOIN nodes n ON n.id = r.node_id
		WHERE r.app_id = ANY($1)
		ORDER BY r.app_id, r.node_id, r.ordinal`, appIDs)
	if err != nil {
		return nil, fmt.Errorf("list app replicas: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var appID int64
		var rep Replica
		if err := rows.Scan(&appID, &rep.NodeID, &rep.NodeSlug, &rep.Ordinal, &rep.Container,
			&rep.Name, &rep.Deploy, &rep.Status, &rep.UpdatedAt); err != nil {
			return nil, err
		}
		out[appID] = append(out[appID], rep)
	}
	return out, rows.Err()
}

// SetNodes replaces the set of machines an app runs on, and how many
// copies are spread over them.
//
// One statement per direction rather than a delete-and-insert: a
// machine that stays keeps its row, which is what keeps the container
// it is already running from being forgotten and started again beside
// itself. A machine that goes has its row deleted, and the app stops
// being in that machine's answer on its next pass — which is how the
// container there is removed.
//
// ErrNoSuchNode when a name is not a machine in this cluster. Refused
// by name rather than written as a null the column would reject with a
// message nobody can read.
func (r *Repository) SetNodes(ctx context.Context, appID int64, nodeSlugs []string, scale, replicas int, spread bool) error {
	if len(nodeSlugs) == 0 {
		return ErrNoSuchNode
	}
	var ids []int64
	rows, err := r.q.QueryContext(ctx, `SELECT id FROM nodes WHERE slug = ANY($1)`, nodeSlugs)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) != len(nodeSlugs) {
		return ErrNoSuchNode
	}

	// What was asked for, kept apart from what there is: zero is "one
	// per machine", and it is the answer that has to survive a machine
	// being added or taken away.
	//
	// Whether it follows the cluster is written here too, because it is
	// the same decision seen from one step back: what these machines
	// are is either a set somebody chose or "all of them", and the two
	// have to be written together or a re-spread could read a stale
	// answer to which it was.
	if _, err := r.q.ExecContext(ctx,
		`UPDATE apps SET scale = $2, spread = $3 WHERE id = $1`, appID, scale, spread); err != nil {
		return fmt.Errorf("record how many copies were asked for: %w", err)
	}
	if _, err := r.q.ExecContext(ctx,
		`DELETE FROM app_nodes WHERE app_id = $1 AND node_id <> ALL($2)`, appID, ids); err != nil {
		return fmt.Errorf("take an app off a machine: %w", err)
	}

	// One row per copy, and the spread decides how many land on each.
	// Ordinals are dense from 1 so that scaling down removes the
	// highest — the machine's answer stops naming that container and
	// the agent removes it, which is the same path a machine that lost
	// an app entirely goes down.
	for i, want := range Spread(replicas, len(ids)) {
		if _, err := r.q.ExecContext(ctx,
			`INSERT INTO app_nodes (app_id, node_id, ordinal)
			 SELECT $1, $2, generate_series(1, $3)
			 ON CONFLICT (app_id, node_id, ordinal) DO NOTHING`, appID, ids[i], want); err != nil {
			return fmt.Errorf("put an app on a machine: %w", err)
		}
		if _, err := r.q.ExecContext(ctx,
			`DELETE FROM app_nodes WHERE app_id = $1 AND node_id = $2 AND ordinal > $3`,
			appID, ids[i], want); err != nil {
			return fmt.Errorf("take a copy off a machine: %w", err)
		}
	}
	return nil
}

// Autoscaling is every app this instance decides the replica count for.
//
// Scoped, because acting on one means placing it — which needs its
// project and environment to build a container name — and a listing of
// a handful of apps is cheaper than resolving each one after.
func (r *Repository) Autoscaling(ctx context.Context) ([]*Scoped, error) {
	return r.listScoped(ctx, scopedQuery+` WHERE a.autoscale_max > 0 ORDER BY a.id`)
}

// MarkAutoscaled records that the rule just changed this app's count,
// which is what its cooldown is measured from.
//
// A row rather than something held in memory: a daemon restart would
// otherwise be a free pass to act again immediately, and a restart is
// exactly what an upgrade is — which is when load is already moving
// between machines.
func (r *Repository) MarkAutoscaled(ctx context.Context, appID int64) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE apps SET autoscaled_at = now() WHERE id = $1`, appID); err != nil {
		return fmt.Errorf("record that the app was scaled: %w", err)
	}
	return nil
}

// EverySlug is every machine in the cluster, by name, the control plane
// included. It is what an app that follows the cluster is placed on.
//
// `without` is a machine to leave out, for the one caller that asks
// before a row is deleted: an app has to leave a machine that is going
// away, and reading the table would still find it there. Zero leaves
// nothing out.
func (r *Repository) EverySlug(ctx context.Context, without int64) ([]string, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT slug FROM nodes WHERE id <> $1 ORDER BY id`, without)
	if err != nil {
		return nil, fmt.Errorf("read the cluster's machines: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// Following is every app that follows the cluster, by id. What has to
// be re-spread when a machine is added or taken away.
func (r *Repository) Following(ctx context.Context) ([]int64, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT id FROM apps WHERE spread ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read the apps that follow the cluster: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ControlPlaneID is the machine this daemon is, by id.
//
// Read rather than configured: the row is seeded by the migration that
// created the table, and the one thing this module needs it for is
// telling its own replicas from the ones somebody else runs. Reading
// the `nodes` table directly is what the scoped query already does.
func (r *Repository) ControlPlaneID(ctx context.Context) (int64, error) {
	var id int64
	if err := r.q.QueryRowContext(ctx,
		`SELECT id FROM nodes WHERE control_plane`).Scan(&id); err != nil {
		return 0, fmt.Errorf("find the control plane: %w", err)
	}
	return id, nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	apps := make([]*App, 0, len(out))
	for _, s := range out {
		apps = append(apps, &s.App)
	}
	return out, r.attach(ctx, apps)
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
