package project

import (
	"context"
	"fmt"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
)

// EnvironmentRepository reads and writes the environments inside
// projects.
type EnvironmentRepository struct {
	q database.Queryer
}

func NewEnvironmentRepository(q database.Queryer) *EnvironmentRepository {
	return &EnvironmentRepository{q: q}
}

const environmentColumns = `id, project_id, slug, name, env, created_at`

func scanEnvironment(row scanner) (*Environment, error) {
	var e Environment
	var envJSON []byte
	if err := row.Scan(&e.ID, &e.ProjectID, &e.Slug, &e.Name, &envJSON, &e.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &e.Env); err != nil {
		return nil, fmt.Errorf("decode env for environment %q: %w", e.Slug, err)
	}
	return &e, nil
}

func (r *EnvironmentRepository) Create(ctx context.Context, projectID int64, slug, name string) (*Environment, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO environments (project_id, slug, name) VALUES ($1, $2, $3) RETURNING `+environmentColumns,
		projectID, slug, name)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}
	return e, nil
}

func (r *EnvironmentRepository) BySlug(ctx context.Context, projectID int64, slug string) (*Environment, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE project_id = $1 AND slug = $2`, projectID, slug)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("get environment %q: %w", slug, err)
	}
	return e, nil
}

func (r *EnvironmentRepository) ByID(ctx context.Context, id int64) (*Environment, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = $1`, id)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("get environment %d: %w", id, err)
	}
	return e, nil
}

func (r *EnvironmentRepository) ListForProject(ctx context.Context, projectID int64) ([]*Environment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE project_id = $1 ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEnv replaces the environment's variables wholesale. Callers that
// mean "add these" want MergeEnv.
func (r *EnvironmentRepository) SetEnv(ctx context.Context, environmentID int64, env envvar.Map) error {
	envJSON, err := envvar.MarshalJSONB(env)
	if err != nil {
		return err
	}
	if _, err := r.q.ExecContext(ctx,
		`UPDATE environments SET env = $1::jsonb WHERE id = $2`, envJSON, environmentID); err != nil {
		return fmt.Errorf("set environment env: %w", err)
	}
	return nil
}

// CountApps reports how many apps live in environmentID. Deleting an
// environment is refused while any remain.
//
// This is the one place this module reads the apps table. It stays here
// rather than in internal/app because the rule it serves — an
// environment cannot be deleted out from under its apps — is the
// environment's invariant, and having app depend on project (it already
// does) while project depends on app would be a cycle.
func (r *EnvironmentRepository) CountApps(ctx context.Context, environmentID int64) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM apps WHERE environment_id = $1`, environmentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count apps in environment: %w", err)
	}
	return n, nil
}

func (r *EnvironmentRepository) Delete(ctx context.Context, environmentID int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM environments WHERE id = $1`, environmentID); err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	return nil
}

// MergeEnv sets the given variables and removes the unset ones, leaving
// every other key alone.
func (r *EnvironmentRepository) MergeEnv(ctx context.Context, environmentID int64, set envvar.Map, unset []string) error {
	setJSON, err := envvar.MarshalJSONB(set)
	if err != nil {
		return err
	}
	return database.MergeJSONBMap(ctx, r.q, "environments", "env", environmentID, setJSON, unset)
}
