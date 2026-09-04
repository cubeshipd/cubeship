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

// appColumns is the column list every app query selects, in the order
// scanApp reads them.
const appColumns = `id, org_id, project_id, environment_id, name, domain, image, container_id, status, env, created_at`

func (s *Store) CreateApp(ctx context.Context, orgID, projectID, environmentID int64, name, domain, image string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO apps (org_id, project_id, environment_id, name, domain, image)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+appColumns,
		orgID, projectID, environmentID, name, domain, image)
	a, err := scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return a, nil
}

func scanApp(row interface{ Scan(dest ...any) error }) (*App, error) {
	var a App
	var envJSON []byte
	if err := row.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.EnvironmentID, &a.Name, &a.Domain, &a.Image, &a.ContainerID, &a.Status, &envJSON, &a.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(envJSON, &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	return &a, nil
}

func (s *Store) GetAppByName(ctx context.Context, name string) (*App, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE name = $1`, name)
	a, err := scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app %q: %w", name, err)
	}
	return a, nil
}

func (s *Store) GetAppByImage(ctx context.Context, image string) (*App, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+appColumns+` FROM apps WHERE image = $1`, image)
	a, err := scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app by image %q: %w", image, err)
	}
	return a, nil
}

func (s *Store) ListApps(ctx context.Context) ([]*App, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+appColumns+` FROM apps ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []*App
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (s *Store) UpdateAppContainer(ctx context.Context, appID int64, containerID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE apps SET container_id = $1, status = $2 WHERE id = $3`,
		containerID, status, appID)
	if err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	return nil
}

func (s *Store) SetAppEnv(ctx context.Context, appID int64, env map[string]string) error {
	envJSON, err := marshalEnv(env)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE apps SET env = $1::jsonb WHERE id = $2`, envJSON, appID); err != nil {
		return fmt.Errorf("set app env: %w", err)
	}
	return nil
}

// marshalEnv encodes an env map for a JSONB column. A nil map becomes
// "{}" rather than JSON null, which the NOT NULL column would reject and
// which every reader would then have to special-case.
func marshalEnv(env map[string]string) (string, error) {
	if env == nil {
		env = map[string]string{}
	}
	b, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("encode env: %w", err)
	}
	return string(b), nil
}
