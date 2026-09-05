package project

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

const columns = `id, org_id, slug, description, env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Project, error) {
	var p Project
	var envJSON []byte
	if err := row.Scan(&p.ID, &p.OrgID, &p.Slug, &p.Description, &envJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &p.Env); err != nil {
		return nil, fmt.Errorf("decode env for project %q: %w", p.Slug, err)
	}
	return &p, nil
}

func (r *Repository) Create(ctx context.Context, orgID int64, slug string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO projects (org_id, slug) VALUES ($1, $2) RETURNING `+columns,
		orgID, slug)
	p, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// Update changes a project's editable fields. A nil argument leaves the
// column alone, so PATCH with one field named cannot blank the other —
// and it stays one statement rather than a read-modify-write.
//
// The slug is not among them, here or anywhere: see Service.Update.
func (r *Repository) Update(ctx context.Context, projectID int64, description *string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`UPDATE projects SET description = COALESCE($1, description)
		 WHERE id = $2 RETURNING `+columns,
		description, projectID)
	p, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return p, nil
}

func (r *Repository) BySlug(ctx context.Context, orgID int64, slug string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM projects WHERE org_id = $1 AND slug = $2`, orgID, slug)
	p, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get project %q: %w", slug, err)
	}
	return p, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Project, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM projects WHERE id = $1`, id)
	p, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("get project %d: %w", id, err)
	}
	return p, nil
}

func (r *Repository) ListForOrg(ctx context.Context, orgID int64) ([]*Project, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM projects WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Project
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetEnv replaces the project's variables wholesale. Callers that mean
// "add these" want MergeEnv.
func (r *Repository) SetEnv(ctx context.Context, projectID int64, env envvar.Map) error {
	envJSON, err := envvar.MarshalJSONB(env)
	if err != nil {
		return err
	}
	if _, err := r.q.ExecContext(ctx,
		`UPDATE projects SET env = $1::jsonb WHERE id = $2`, envJSON, projectID); err != nil {
		return fmt.Errorf("set project env: %w", err)
	}
	return nil
}

// MergeEnv sets the given variables and removes the unset ones, leaving
// every other key alone.
func (r *Repository) MergeEnv(ctx context.Context, projectID int64, set envvar.Map, unset []string) error {
	setJSON, err := envvar.MarshalJSONB(set)
	if err != nil {
		return err
	}
	return database.MergeJSONBMap(ctx, r.q, "projects", "env", projectID, setJSON, unset)
}

// CountApps reports how many apps live anywhere in a project.
//
// Like EnvironmentRepository.CountApps, this reads the apps table from
// the project module because the rule it serves is the project's
// invariant — a project cannot be deleted out from under its apps — and
// internal/app already depends on this package.
func (r *Repository) CountApps(ctx context.Context, projectID int64) (int, error) {
	var n int
	if err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM apps WHERE project_id = $1`, projectID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count apps in project: %w", err)
	}
	return n, nil
}

// Delete removes a project and the environments inside it. The caller
// must have established that no apps remain.
func (r *Repository) Delete(ctx context.Context, projectID int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM environments WHERE project_id = $1`, projectID); err != nil {
		return fmt.Errorf("delete environments: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}
