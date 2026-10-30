package app

import (
	"context"
	"encoding/json"
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

const columns = `id, project_id, environment_id, name, source, source_image,
	 source_tag, source_repo, source_ref, source_dockerfile, health_path, internal_ports, health_checks, scale, spread,
	cpu_limit, memory_limit,
	autoscale_min, autoscale_max, autoscale_cpu, autoscaled_at,
	env, predeploy_command, predeploy_timeout, stop_grace_period, stop_signal, watch_paths, ignore_paths, build_args, created_at`

type scanner interface{ Scan(dest ...any) error }

// dests is where every column in `columns` lands, in that order.
//
// **One list, read by both scans.** The scoped query below joins, so it
// has to qualify the names — and the moment those are written out a
// second time, a column added to one and not the other is a value that
// silently comes back empty. `source_tag` did exactly that: the scoped
// read never selected it, so every app reported no tag, every pinned
// app reported `autodeploy`, and the registry webhook redeployed apps
// somebody had pinned to a version. Nothing failed; it just stopped
// being true.
func dests(a *App, envJSON, commandJSON, watchJSON, ignoreJSON, buildJSON, portsJSON, checksJSON *[]byte, timeout *int64, stopGrace *int64, stopSignal *string) []any {
	return []any{
		&a.ID, &a.ProjectID, &a.EnvironmentID, &a.Name,
		&a.Source, &a.SourceImage, &a.SourceTag, &a.SourceRepo, &a.SourceRef, &a.SourceDockerfile,
		&a.HealthPath, portsJSON, checksJSON, &a.Scale, &a.Spread,
		&a.Limits.CPU, &a.Limits.Memory,
		&a.Autoscale.Min, &a.Autoscale.Max, &a.Autoscale.CPU, &a.Autoscale.At,
		envJSON, commandJSON, timeout, stopGrace, stopSignal, watchJSON, ignoreJSON, buildJSON, &a.CreatedAt,
	}
}

// qualify prefixes every name in a column list with a table alias, so a
// join selects the same list the scan reads rather than a copy of it.
func qualify(alias, list string) string {
	names := strings.Split(list, ",")
	for i, name := range names {
		names[i] = alias + "." + strings.TrimSpace(name)
	}
	return strings.Join(names, ", ")
}

func scan(row scanner) (*App, error) {
	var a App
	var envJSON []byte
	var commandJSON []byte
	var timeout int64
	var stopGrace int64
	var stopSignal string
	var watchJSON, ignoreJSON, buildJSON, portsJSON, checksJSON []byte
	if err := row.Scan(dests(&a, &envJSON, &commandJSON, &watchJSON, &ignoreJSON, &buildJSON, &portsJSON, &checksJSON, &timeout, &stopGrace, &stopSignal)...); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	if len(commandJSON) != 0 && string(commandJSON) != "null" {
		if err := json.Unmarshal(commandJSON, &a.Predeploy); err != nil {
			return nil, fmt.Errorf("decode predeploy command for app %q: %w", a.Name, err)
		}
	}
	a.Predeploy.Timeout = time.Duration(timeout) * time.Second
	a.StopGracePeriod = time.Duration(stopGrace) * time.Second
	a.StopSignal = stopSignal
	if len(portsJSON) != 0 && string(portsJSON) != "null" {
		if err := json.Unmarshal(portsJSON, &a.InternalPorts); err != nil {
			return nil, fmt.Errorf("decode internal ports for app %q: %w", a.Name, err)
		}
	}
	if len(checksJSON) != 0 && string(checksJSON) != "null" {
		if err := json.Unmarshal(checksJSON, &a.HealthChecks); err != nil {
			return nil, fmt.Errorf("decode health checks for app %q: %w", a.Name, err)
		}
	}
	if len(watchJSON) != 0 && string(watchJSON) != "null" {
		if err := json.Unmarshal(watchJSON, &a.WatchPaths); err != nil {
			return nil, fmt.Errorf("decode watch paths for app %q: %w", a.Name, err)
		}
	}
	if len(ignoreJSON) != 0 && string(ignoreJSON) != "null" {
		if err := json.Unmarshal(ignoreJSON, &a.IgnorePaths); err != nil {
			return nil, fmt.Errorf("decode ignore paths for app %q: %w", a.Name, err)
		}
	}
	if len(buildJSON) != 0 && string(buildJSON) != "null" {
		if err := json.Unmarshal(buildJSON, &a.BuildArgs); err != nil {
			return nil, fmt.Errorf("decode build args for app %q: %w", a.Name, err)
		}
	}
	return &a, nil
}

// Update changes an app's configuration: what it is, where it is
// served, and where its image comes from. A nil field is left alone, so
// saving one section of the settings screen cannot blank another.
//
// The slug is not here. It is the last component of the app's registry
// reference, and no slug in Cubeship changes once its resource exists.
func (r *Repository) Update(ctx context.Context, appID int64, source *Source, origin *Origin, health *string, limits *Limits, auto *Autoscale, predeploy ...*Predeploy) (*App, error) {
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
	var command any
	var timeout any
	if len(predeploy) > 0 && predeploy[0] != nil {
		b, err := json.Marshal(predeploy[0])
		if err != nil {
			return nil, fmt.Errorf("encode predeploy command: %w", err)
		}
		command = string(b)
		timeout = int64(predeploy[0].Timeout / time.Second)
	}
	row := r.q.QueryRowContext(ctx,
		`UPDATE apps SET
		   source            = COALESCE($1, source),
		   source_image      = COALESCE($2, source_image),
		   source_tag        = COALESCE($3, source_tag),
		   source_repo       = COALESCE($4, source_repo),
		   source_ref        = COALESCE($5, source_ref),
		   source_dockerfile = COALESCE($6, source_dockerfile),
		   health_path       = COALESCE($7, health_path),
		   cpu_limit         = COALESCE($8, cpu_limit),
		   memory_limit      = COALESCE($9, memory_limit),
		   autoscale_min     = COALESCE($10, autoscale_min),
		   autoscale_max     = COALESCE($11, autoscale_max),
		   autoscale_cpu     = COALESCE($12, autoscale_cpu),
		   predeploy_command = COALESCE($13::jsonb, predeploy_command),
		   predeploy_timeout = COALESCE($14, predeploy_timeout)
		 WHERE id = $15 RETURNING `+columns,
		src, image, tag, repo, ref, dockerfile, health, cpu, memory,
		autoMin, autoMax, autoCPU, command, timeout, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update app: %w", err)
	}
	return a, r.attach(ctx, []*App{a})
}

func (r *Repository) UpdateStopPolicy(ctx context.Context, appID int64, grace *int64, signal *string) (*App, error) {
	row := r.q.QueryRowContext(ctx, `UPDATE apps SET
		stop_grace_period = COALESCE($1, stop_grace_period),
		stop_signal = COALESCE($2, stop_signal)
		WHERE id = $3 RETURNING `+columns, grace, signal, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update stop policy: %w", err)
	}
	return a, r.attach(ctx, []*App{a})
}

func (r *Repository) UpdatePathFilters(ctx context.Context, appID int64, watch, ignore []string) (*App, error) {
	watchJSON, err := json.Marshal(watch)
	if err != nil {
		return nil, fmt.Errorf("encode watch paths: %w", err)
	}
	ignoreJSON, err := json.Marshal(ignore)
	if err != nil {
		return nil, fmt.Errorf("encode ignore paths: %w", err)
	}
	row := r.q.QueryRowContext(ctx, `UPDATE apps SET watch_paths = $1::jsonb, ignore_paths = $2::jsonb WHERE id = $3 RETURNING `+columns, watchJSON, ignoreJSON, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update path filters: %w", err)
	}
	return a, r.attach(ctx, []*App{a})
}

func (r *Repository) UpdateBuildArgs(ctx context.Context, appID int64, args map[string]string) (*App, error) {
	b, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	row := r.q.QueryRowContext(ctx, `UPDATE apps SET build_args = $1::jsonb WHERE id = $2 RETURNING `+columns, b, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update build args: %w", err)
	}
	return a, r.attach(ctx, []*App{a})
}

func (r *Repository) UpdateNetworking(ctx context.Context, appID int64, ports []int, checks []HealthCheck) (*App, error) {
	portsJSON, err := json.Marshal(ports)
	if err != nil {
		return nil, fmt.Errorf("encode internal ports: %w", err)
	}
	checksJSON, err := json.Marshal(checks)
	if err != nil {
		return nil, fmt.Errorf("encode health checks: %w", err)
	}
	row := r.q.QueryRowContext(ctx, `UPDATE apps
		SET internal_ports = $1::jsonb, health_checks = $2::jsonb,
		    health_path = COALESCE((SELECT item->>'path' FROM jsonb_array_elements($2::jsonb) item LIMIT 1), '')
		WHERE id = $3 RETURNING `+columns, portsJSON, checksJSON, appID)
	a, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update app networking: %w", err)
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

func (r *Repository) Create(ctx context.Context, projectID, environmentID int64, name string, source Source, origin Origin) (*App, error) {
	row := r.q.QueryRowContext(ctx,
		// An app is created on the machine the daemon is on, and moved
		// afterwards if it belongs somewhere else. The subquery rather
		// than a column default because a default would have to name a
		// row by a number, and the control plane's is a fact about a
		// table rather than a constant.
		`INSERT INTO apps (project_id, environment_id, name, source,
		                   source_image, source_tag, source_repo, source_ref, source_dockerfile)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+columns,
		projectID, environmentID, name, string(source),
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
