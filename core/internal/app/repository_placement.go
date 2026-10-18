package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cubeship/internal/envvar"
)

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

// scopedQuery selects an app with its containing slugs, from the same
// column list `scan` reads — aliased rather than spelled out again.
var scopedQuery = `
	SELECT ` + qualify("a", columns) + `, p.slug, e.slug
	FROM apps a
	JOIN projects p ON p.id = a.project_id
	JOIN environments e ON e.id = a.environment_id`

func scanScoped(row scanner) (*Scoped, error) {
	var s Scoped
	var envJSON []byte
	var commandJSON []byte
	var timeout int64
	var stopGrace int64
	var stopSignal string
	var watchJSON, ignoreJSON, buildJSON []byte
	into := append(dests(&s.App, &envJSON, &commandJSON, &watchJSON, &ignoreJSON, &buildJSON, &timeout, &stopGrace, &stopSignal), &s.ProjectSlug, &s.EnvironmentSlug)
	if err := row.Scan(into...); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &s.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", s.Name, err)
	}
	if len(commandJSON) != 0 && string(commandJSON) != "null" {
		if err := json.Unmarshal(commandJSON, &s.Predeploy); err != nil {
			return nil, fmt.Errorf("decode predeploy command for app %q: %w", s.Name, err)
		}
	}
	s.Predeploy.Timeout = time.Duration(timeout) * time.Second
	if len(buildJSON) != 0 && string(buildJSON) != "null" {
		_ = json.Unmarshal(buildJSON, &s.BuildArgs)
	}
	s.StopGracePeriod = time.Duration(stopGrace) * time.Second
	s.StopSignal = stopSignal
	if len(watchJSON) != 0 && string(watchJSON) != "null" {
		if err := json.Unmarshal(watchJSON, &s.WatchPaths); err != nil {
			return nil, fmt.Errorf("decode watch paths for app %q: %w", s.Name, err)
		}
	}
	if len(ignoreJSON) != 0 && string(ignoreJSON) != "null" {
		if err := json.Unmarshal(ignoreJSON, &s.IgnorePaths); err != nil {
			return nil, fmt.Errorf("decode ignore paths for app %q: %w", s.Name, err)
		}
	}
	return &s, nil
}

// BuildingFromRepository finds every app that builds from a repository
// and has named no ref of its own.
//
// GitHub's legacy webhook names "owner/name". Other providers pass a
// qualified host and path so same-named repositories cannot overlap.
//
// An app with no ref tracks whatever branch it is told about. A named ref
// tracks only that branch; the webhook branch is still the commit-ish passed
// to the build.
func (r *Repository) BuildingFromRepository(ctx context.Context, fullName, branch string) ([]*Scoped, error) {
	rows, err := r.q.QueryContext(ctx, scopedQuery+`
		WHERE a.source = ANY($1)
		  AND lower(regexp_replace(regexp_replace(a.source_repo, '^https?://(www\.)?', ''), '(\.git)?/?$', '')) = lower(regexp_replace(regexp_replace(CASE WHEN $2 LIKE '%://%' OR $2 LIKE 'bitbucket.org/%' OR $2 LIKE 'github.com/%' THEN $2 ELSE 'github.com/' || $2 END, '^https?://(www\.)?', ''), '(\.git)?/?$', ''))
		  AND (a.source_ref = '' OR lower(a.source_ref) = lower($3))`,
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
