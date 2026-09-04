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

const columns = `id, org_id, slug, name, env, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Project, error) {
	var p Project
	var envJSON []byte
	if err := row.Scan(&p.ID, &p.OrgID, &p.Slug, &p.Name, &envJSON, &p.CreatedAt); err != nil {
		return nil, err
	}
	if err := envvar.UnmarshalJSONB(envJSON, &p.Env); err != nil {
		return nil, fmt.Errorf("decode env for project %q: %w", p.Slug, err)
	}
	return &p, nil
}

func (r *Repository) Create(ctx context.Context, orgID int64, slug, name string) (*Project, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO projects (org_id, slug, name) VALUES ($1, $2, $3) RETURNING `+columns,
		orgID, slug, name)
	p, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
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
