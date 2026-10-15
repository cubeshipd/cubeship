package templateinstall

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"cubeship/internal/platform/database"
	"cubeship/template"
)

// Repository reads and writes template_installs and their runs.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const installColumns = `id, owner, repo, release, commit_sha, project, environment, status,
	manifest, answers, resources, created_by, created_at, updated_at`

const runColumns = `id, install_id, kind, from_release, to_release, status, step, error,
	created, snapshot, keep_data, created_by, created_at, finished_at`

type scanner interface{ Scan(dest ...any) error }

func scanInstall(row scanner) (*Install, error) {
	var in Install
	var manifest, answers, resources []byte
	var createdBy sql.NullInt64
	if err := row.Scan(&in.ID, &in.Owner, &in.Repo, &in.Release, &in.Commit, &in.Project, &in.Environment,
		&in.Status, &manifest, &answers, &resources, &createdBy, &in.CreatedAt, &in.UpdatedAt); err != nil {
		return nil, err
	}
	if string(manifest) != "null" {
		in.Manifest = &template.Normalized{}
		if err := json.Unmarshal(manifest, in.Manifest); err != nil {
			return nil, fmt.Errorf("read installation %d manifest: %w", in.ID, err)
		}
	}
	if err := json.Unmarshal(answers, &in.Answers); err != nil {
		return nil, fmt.Errorf("read installation %d answers: %w", in.ID, err)
	}
	if err := json.Unmarshal(resources, &in.Resources); err != nil {
		return nil, fmt.Errorf("read installation %d resources: %w", in.ID, err)
	}
	in.CreatedBy = createdBy.Int64
	return &in, nil
}

func scanRun(row scanner) (*Run, error) {
	var r Run
	var created, snapshot []byte
	var createdBy sql.NullInt64
	var finished sql.NullTime
	if err := row.Scan(&r.ID, &r.InstallID, &r.Kind, &r.FromRelease, &r.ToRelease, &r.Status, &r.Step, &r.Error,
		&created, &snapshot, &r.KeepData, &createdBy, &r.CreatedAt, &finished); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(created, &r.Created); err != nil {
		return nil, fmt.Errorf("read run %d: %w", r.ID, err)
	}
	if err := json.Unmarshal(snapshot, &r.Snapshot); err != nil {
		return nil, fmt.Errorf("read run %d: %w", r.ID, err)
	}
	r.CreatedBy = createdBy.Int64
	if finished.Valid {
		r.FinishedAt = &finished.Time
	}
	return &r, nil
}

// jsonOf writes a value for a JSONB column, with nil slices and maps as
// empty ones rather than null.
func jsonOf(v any, empty string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte(empty), nil
	}
	return b, nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func installJSON(in *Install) (manifest, answers, resources []byte, err error) {
	if manifest, err = jsonOf(in.Manifest, "null"); err != nil {
		return
	}
	if answers, err = jsonOf(in.Answers, "{}"); err != nil {
		return
	}
	resources, err = jsonOf(in.Resources, "[]")
	return
}

func (r *Repository) CreateInstall(ctx context.Context, in *Install) (*Install, error) {
	manifest, answers, resources, err := installJSON(in)
	if err != nil {
		return nil, err
	}
	created, err := scanInstall(r.q.QueryRowContext(ctx, `
		INSERT INTO template_installs (owner, repo, release, commit_sha, project, environment, status, manifest, answers, resources, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING `+installColumns,
		in.Owner, in.Repo, in.Release, in.Commit, in.Project, in.Environment, in.Status,
		manifest, answers, resources, nullID(in.CreatedBy)))
	if err != nil {
		return nil, fmt.Errorf("record installation: %w", err)
	}
	return created, nil
}

func (r *Repository) SaveInstall(ctx context.Context, in *Install) error {
	manifest, answers, resources, err := installJSON(in)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		UPDATE template_installs
		SET release = $2, commit_sha = $3, status = $4, manifest = $5, answers = $6, resources = $7, updated_at = now()
		WHERE id = $1`,
		in.ID, in.Release, in.Commit, in.Status, manifest, answers, resources)
	return err
}

func (r *Repository) Install(ctx context.Context, id int64) (*Install, error) {
	in, err := scanInstall(r.q.QueryRowContext(ctx, `SELECT `+installColumns+` FROM template_installs WHERE id = $1`, id))
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return in, err
}

// Installs is the most recent installations, newest first.
func (r *Repository) Installs(ctx context.Context) ([]*Install, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+installColumns+` FROM template_installs ORDER BY created_at DESC, id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Install{}
	for rows.Next() {
		in, err := scanInstall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func runJSON(run *Run) (created, snapshot []byte, err error) {
	if created, err = jsonOf(run.Created, "[]"); err != nil {
		return
	}
	snapshot, err = jsonOf(run.Snapshot, "[]")
	return
}

func (r *Repository) CreateRun(ctx context.Context, run *Run) (*Run, error) {
	created, snapshot, err := runJSON(run)
	if err != nil {
		return nil, err
	}
	out, err := scanRun(r.q.QueryRowContext(ctx, `
		INSERT INTO template_install_runs (install_id, kind, from_release, to_release, status, step, created, snapshot, keep_data, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+runColumns,
		run.InstallID, run.Kind, run.FromRelease, run.ToRelease, run.Status, run.Step,
		created, snapshot, run.KeepData, nullID(run.CreatedBy)))
	if err != nil {
		return nil, fmt.Errorf("record run: %w", err)
	}
	return out, nil
}

func (r *Repository) SaveRun(ctx context.Context, run *Run) error {
	created, snapshot, err := runJSON(run)
	if err != nil {
		return err
	}
	_, err = r.q.ExecContext(ctx, `
		UPDATE template_install_runs
		SET status = $2, step = $3, error = $4, created = $5, snapshot = $6, finished_at = $7
		WHERE id = $1`,
		run.ID, run.Status, run.Step, run.Error, created, snapshot, run.FinishedAt)
	return err
}

// Runs is an installation's most recent runs, newest first.
func (r *Repository) Runs(ctx context.Context, installID int64) ([]*Run, error) {
	return r.runs(ctx, `SELECT `+runColumns+` FROM template_install_runs WHERE install_id = $1 ORDER BY id DESC LIMIT 20`, installID)
}

// RunningRuns is every run still recorded as running.
func (r *Repository) RunningRuns(ctx context.Context) ([]*Run, error) {
	return r.runs(ctx, `SELECT `+runColumns+` FROM template_install_runs WHERE status = 'running' ORDER BY id`)
}

func (r *Repository) runs(ctx context.Context, query string, args ...any) ([]*Run, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Run{}
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
