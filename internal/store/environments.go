package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ProductionEnvSlug is the environment every project is created with. It
// can never be deleted (see api.handleDeleteEnvironment) — apps and
// deploys can always assume at least one environment exists per project.
const ProductionEnvSlug = "production"

type Environment struct {
	ID        int64
	ProjectID int64
	Slug      string
	Name      string
	Env       map[string]string
	CreatedAt time.Time
}

const environmentColumns = `id, project_id, slug, name, env, created_at`

func createEnvironment(ctx context.Context, q queryer, projectID int64, slug, name string) (*Environment, error) {
	row := q.QueryRowContext(ctx,
		`INSERT INTO environments (project_id, slug, name) VALUES ($1, $2, $3) RETURNING `+environmentColumns,
		projectID, slug, name)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("create environment: %w", err)
	}
	return e, nil
}

func (s *Store) CreateEnvironment(ctx context.Context, projectID int64, slug, name string) (*Environment, error) {
	return createEnvironment(ctx, s.db, projectID, slug, name)
}

// CreateEnvironment is createEnvironment inside t's transaction.
func (t *Tx) CreateEnvironment(ctx context.Context, projectID int64, slug, name string) (*Environment, error) {
	return createEnvironment(ctx, t.q, projectID, slug, name)
}

func scanEnvironment(row interface{ Scan(dest ...any) error }) (*Environment, error) {
	var e Environment
	var envJSON []byte
	if err := row.Scan(&e.ID, &e.ProjectID, &e.Slug, &e.Name, &envJSON, &e.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(envJSON, &e.Env); err != nil {
		return nil, fmt.Errorf("decode env for environment %q: %w", e.Slug, err)
	}
	return &e, nil
}

func getEnvironmentBySlug(ctx context.Context, q queryer, projectID int64, slug string) (*Environment, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE project_id = $1 AND slug = $2`, projectID, slug)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("get environment %q: %w", slug, err)
	}
	return e, nil
}

func (s *Store) GetEnvironmentBySlug(ctx context.Context, projectID int64, slug string) (*Environment, error) {
	return getEnvironmentBySlug(ctx, s.db, projectID, slug)
}

func (s *Store) GetEnvironmentByID(ctx context.Context, id int64) (*Environment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+environmentColumns+` FROM environments WHERE id = $1`, id)
	e, err := scanEnvironment(row)
	if err != nil {
		return nil, fmt.Errorf("get environment %d: %w", id, err)
	}
	return e, nil
}

func (s *Store) ListEnvironmentsForProject(ctx context.Context, projectID int64) ([]*Environment, error) {
	rows, err := s.db.QueryContext(ctx,
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

func (s *Store) SetEnvironmentEnv(ctx context.Context, environmentID int64, env map[string]string) error {
	envJSON, err := marshalEnv(env)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE environments SET env = $1::jsonb WHERE id = $2`, envJSON, environmentID); err != nil {
		return fmt.Errorf("set environment env: %w", err)
	}
	return nil
}

// CountAppsInEnvironment reports how many apps currently live in
// environmentID. handleDeleteEnvironment uses this to refuse deleting an
// environment that would orphan apps.
func (s *Store) CountAppsInEnvironment(ctx context.Context, environmentID int64) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM apps WHERE environment_id = $1`, environmentID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count apps in environment: %w", err)
	}
	return n, nil
}

func (s *Store) DeleteEnvironment(ctx context.Context, environmentID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = $1`, environmentID); err != nil {
		return fmt.Errorf("delete environment: %w", err)
	}
	return nil
}
