package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Project struct {
	ID        int64
	OrgID     int64
	Slug      string
	Name      string
	Env       map[string]string
	CreatedAt time.Time
}

const projectColumns = `id, org_id, slug, name, env, created_at`

func createProject(ctx context.Context, q queryer, orgID int64, slug, name string) (*Project, error) {
	row := q.QueryRowContext(ctx,
		`INSERT INTO projects (org_id, slug, name) VALUES ($1, $2, $3) RETURNING `+projectColumns,
		orgID, slug, name)
	p, err := scanProject(row)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *Store) CreateProject(ctx context.Context, orgID int64, slug, name string) (*Project, error) {
	return createProject(ctx, s.db, orgID, slug, name)
}

// CreateProject is createProject inside t's transaction.
func (t *Tx) CreateProject(ctx context.Context, orgID int64, slug, name string) (*Project, error) {
	return createProject(ctx, t.q, orgID, slug, name)
}

// CreateProjectWithDefaultEnvironment creates a project and its mandatory
// ProductionEnvSlug environment atomically: a project with no environment
// yet has nowhere for an app to be created, and a production environment
// without its project would be unreachable through the API.
func (s *Store) CreateProjectWithDefaultEnvironment(ctx context.Context, orgID int64, slug, name string) (*Project, *Environment, error) {
	var project *Project
	var env *Environment
	err := s.WithTx(ctx, func(tx *Tx) error {
		var err error
		project, err = tx.CreateProject(ctx, orgID, slug, name)
		if err != nil {
			return err
		}
		env, err = tx.CreateEnvironment(ctx, project.ID, ProductionEnvSlug, "Production")
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return project, env, nil
}

func scanProject(row interface{ Scan(dest ...any) error }) (*Project, error) {
	var p Project
	var envJSON []byte
	if err := row.Scan(&p.ID, &p.OrgID, &p.Slug, &p.Name, &envJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(envJSON, &p.Env); err != nil {
		return nil, fmt.Errorf("decode env for project %q: %w", p.Slug, err)
	}
	return &p, nil
}

func getProjectBySlug(ctx context.Context, q queryer, orgID int64, slug string) (*Project, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE org_id = $1 AND slug = $2`, orgID, slug)
	p, err := scanProject(row)
	if err != nil {
		return nil, fmt.Errorf("get project %q: %w", slug, err)
	}
	return p, nil
}

func (s *Store) GetProjectBySlug(ctx context.Context, orgID int64, slug string) (*Project, error) {
	return getProjectBySlug(ctx, s.db, orgID, slug)
}

func (s *Store) GetProjectByID(ctx context.Context, id int64) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1`, id)
	p, err := scanProject(row)
	if err != nil {
		return nil, fmt.Errorf("get project %d: %w", id, err)
	}
	return p, nil
}

func (s *Store) ListProjectsForOrg(ctx context.Context, orgID int64) ([]*Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetProjectEnv(ctx context.Context, projectID int64, env map[string]string) error {
	envJSON, err := marshalEnv(env)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE projects SET env = $1::jsonb WHERE id = $2`, envJSON, projectID); err != nil {
		return fmt.Errorf("set project env: %w", err)
	}
	return nil
}
