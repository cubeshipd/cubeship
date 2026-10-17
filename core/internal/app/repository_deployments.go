package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
)

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
	// Volumes too, for the same reason: they decide how an app may be
	// placed and deployed, and a caller that forgot them would move one.
	volumes, err := r.VolumesFor(ctx, ids)
	if err != nil {
		return err
	}
	// And published ports, which pin an app the same way.
	tcp, err := r.TCPPortsFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, a := range apps {
		a.Replicas = byApp[a.ID]
		a.Volumes = volumes[a.ID]
		a.TCPPorts = tcp[a.ID]
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

// SetAppStatus records an intentional stop or resume for every replica.
func (r *Repository) SetAppStatus(ctx context.Context, appID int64, status string) error {
	if _, err := r.q.ExecContext(ctx, `UPDATE app_nodes SET status = $2, updated_at = now() WHERE app_id = $1`, appID, status); err != nil {
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

const deploymentColumns = `id, app_id, image_ref, git_sha, git_branch, status, error, logs, created_at, phase, started_at, finished_at, cancelled`

// deploymentListColumns is the same minus the log itself, which is up
// to MaxDeploymentLogBytes a row against fifty rows of history — and
// which the dashboard would then re-fetch every two seconds while a
// build is running. Whether there *is* one is all a listing needs.
const deploymentListColumns = `id, app_id, image_ref, git_sha, git_branch, status, error, logs <> '' AS has_logs, created_at, phase, started_at, finished_at, cancelled`

func scanDeployment(row scanner) (*Deployment, error) {
	var d Deployment
	if err := row.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.GitSHA, &d.GitBranch, &d.Status, &d.Error, &d.Logs, &d.CreatedAt, &d.Phase, &d.StartedAt, &d.FinishedAt, &d.Cancelled); err != nil {
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
	return r.StartDeploymentWithIdentity(ctx, appID, imageRef, "", "")
}

func (r *Repository) StartDeploymentWithIdentity(ctx context.Context, appID int64, imageRef, gitSHA, gitBranch string) (*Deployment, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO deployments (app_id, image_ref, git_sha, git_branch, status, phase) VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+deploymentColumns,
		appID, imageRef, gitSHA, gitBranch, DeploymentPending, DeploymentPhaseQueued)
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

func (r *Repository) SetDeploymentPhase(ctx context.Context, id int64, phase string) error {
	if _, err := r.q.ExecContext(ctx, `UPDATE deployments SET phase = $1 WHERE id = $2`, phase, id); err != nil {
		return fmt.Errorf("set deployment phase: %w", err)
	}
	return nil
}

// FinishDeployment writes a deploy's outcome.

func (r *Repository) FinishDeployment(ctx context.Context, id int64, status, errMsg string, cancelled ...bool) error {
	isCancelled := len(cancelled) > 0 && cancelled[0]
	phase := DeploymentPhaseDone
	if isCancelled {
		phase = DeploymentPhaseCancelled
	} else if status == DeploymentFailed {
		phase = DeploymentPhaseFailed
	}
	if _, err := r.q.ExecContext(ctx,
		`UPDATE deployments SET status = $1, error = $2, phase = $3, finished_at = now(), cancelled = $4 WHERE id = $5 AND status = $6`, status, errMsg, phase, isCancelled, id, DeploymentPending); err != nil {
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

// AppendDeploymentLogs adds output received from a worker without racing the
// control plane's build log writer.
func (r *Repository) AppendDeploymentLogs(ctx context.Context, id int64, logs string) error {
	if _, err := r.q.ExecContext(ctx,
		`UPDATE deployments SET logs = logs || $1 WHERE id = $2`, logs, id); err != nil {
		return fmt.Errorf("append deployment logs: %w", err)
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
	if err := row.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.GitSHA, &d.GitBranch, &d.Status, &d.Error, &d.HasLogs, &d.CreatedAt, &d.Phase, &d.StartedAt, &d.FinishedAt, &d.Cancelled); err != nil {
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
