package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type App struct {
	ID            int64
	OrgID         int64
	ProjectID     int64
	EnvironmentID int64
	Name          string
	Domain        string
	Image         string
	ContainerID   string
	Status        string
	Env           map[string]string
	CreatedAt     time.Time
}

func (s *Store) CreateApp(ctx context.Context, orgID, projectID, environmentID int64, name, domain, image string) (*App, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO apps (org_id, project_id, environment_id, name, domain, image) VALUES (?, ?, ?, ?, ?, ?)`,
		orgID, projectID, environmentID, name, domain, image); err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return s.GetAppByName(ctx, name)
}

func (s *Store) scanApp(row interface {
	Scan(dest ...any) error
}) (*App, error) {
	var a App
	var envJSON string
	if err := row.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.EnvironmentID, &a.Name, &a.Domain, &a.Image, &a.ContainerID, &a.Status, &envJSON, &a.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(envJSON), &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	return &a, nil
}

func (s *Store) GetAppByName(ctx context.Context, name string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, project_id, environment_id, name, domain, image, container_id, status, env, created_at FROM apps WHERE name = ?`, name)
	a, err := s.scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app %q: %w", name, err)
	}
	return a, nil
}

func (s *Store) GetAppByImage(ctx context.Context, image string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, org_id, project_id, environment_id, name, domain, image, container_id, status, env, created_at FROM apps WHERE image = ?`, image)
	a, err := s.scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app by image %q: %w", image, err)
	}
	return a, nil
}

func (s *Store) ListApps(ctx context.Context) ([]*App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, org_id, project_id, environment_id, name, domain, image, container_id, status, env, created_at FROM apps ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []*App
	for rows.Next() {
		a, err := s.scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (s *Store) UpdateAppContainer(ctx context.Context, appID int64, containerID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE apps SET container_id = ?, status = ? WHERE id = ?`,
		containerID, status, appID)
	if err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	return nil
}

func (s *Store) SetAppEnv(ctx context.Context, appID int64, env map[string]string) error {
	envJSON, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode env: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE apps SET env = ? WHERE id = ?`, string(envJSON), appID); err != nil {
		return fmt.Errorf("set app env: %w", err)
	}
	return nil
}
