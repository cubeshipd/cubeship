package templateinstall

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes template_installs.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const columns = `id, owner, repo, release, commit_sha, project, environment, status, step, error,
	resources, created_by, created_at, finished_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Install, error) {
	var in Install
	var resources []byte
	var createdBy sql.NullInt64
	var finished sql.NullTime
	if err := row.Scan(&in.ID, &in.Owner, &in.Repo, &in.Release, &in.Commit, &in.Project, &in.Environment,
		&in.Status, &in.Step, &in.Error, &resources, &createdBy, &in.CreatedAt, &finished); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resources, &in.Resources); err != nil {
		return nil, fmt.Errorf("read install %d resources: %w", in.ID, err)
	}
	in.CreatedBy = createdBy.Int64
	if finished.Valid {
		in.FinishedAt = &finished.Time
	}
	return &in, nil
}

func resourcesJSON(rs []Resource) ([]byte, error) {
	if rs == nil {
		rs = []Resource{}
	}
	return json.Marshal(rs)
}

func (r *Repository) Create(ctx context.Context, in *Install) (*Install, error) {
	resources, err := resourcesJSON(in.Resources)
	if err != nil {
		return nil, err
	}
	var createdBy any
	if in.CreatedBy != 0 {
		createdBy = in.CreatedBy
	}
	row := r.q.QueryRowContext(ctx, `
		INSERT INTO template_installs (owner, repo, release, commit_sha, project, environment, status, step, resources, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+columns,
		in.Owner, in.Repo, in.Release, in.Commit, in.Project, in.Environment, in.Status, in.Step, resources, createdBy)
	created, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("record install: %w", err)
	}
	return created, nil
}

// Save writes what an install changes as it runs.
func (r *Repository) Save(ctx context.Context, in *Install) error {
	resources, err := resourcesJSON(in.Resources)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		UPDATE template_installs
		SET status = $2, step = $3, error = $4, resources = $5, finished_at = $6
		WHERE id = $1`,
		in.ID, in.Status, in.Step, in.Error, resources, in.FinishedAt)
	return err
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Install, error) {
	in, err := scan(r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM template_installs WHERE id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return in, err
}

// List is the most recent installs, newest first.
func (r *Repository) List(ctx context.Context, limit int) ([]*Install, error) {
	return r.query(ctx, `SELECT `+columns+` FROM template_installs ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
}

// Running is every install still recorded as running.
func (r *Repository) Running(ctx context.Context) ([]*Install, error) {
	return r.query(ctx, `SELECT `+columns+` FROM template_installs WHERE status = 'running' ORDER BY id`)
}

func (r *Repository) query(ctx context.Context, query string, args ...any) ([]*Install, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Install{}
	for rows.Next() {
		in, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
