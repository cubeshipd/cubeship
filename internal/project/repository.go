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

const columns = `id, slug, description, env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Project, error) {
	var p Project
	var envJSON []byte
	if err := row.Scan(&p.ID, &p.Slug, &p.Description, &envJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &p.Env); err != nil {
		return nil, fmt.Errorf("decode env for project %q: %w", p.Slug, err)
	}
	return &p, nil
}

func (r *Repository) Create(ctx context.Context, slug string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO projects (slug) VALUES ($1) RETURNING `+columns, slug)
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

func (r *Repository) BySlug(ctx context.Context, slug string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM projects WHERE slug = $1`, slug)
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

func (r *Repository) List(ctx context.Context) ([]*Project, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM projects ORDER BY id`)
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

// Delete removes a project and the environments inside it. The apps must
// already be gone — stopping their containers is the app module's job,
// which is what AppTeardown is for.
func (r *Repository) Delete(ctx context.Context, projectID int64) error {
	if _, err := r.q.ExecContext(ctx, `DELETE FROM environments WHERE project_id = $1`, projectID); err != nil {
		return fmt.Errorf("delete environments: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}
