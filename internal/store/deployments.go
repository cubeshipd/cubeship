package store

import (
	"context"
	"fmt"
	"time"
)

type Deployment struct {
	ID        int64
	AppID     int64
	ImageRef  string
	Status    string
	Error     string
	CreatedAt time.Time
}

func (s *Store) RecordDeployment(ctx context.Context, appID int64, imageRef, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, image_ref, status, error) VALUES ($1, $2, $3, $4)`,
		appID, imageRef, status, errMsg)
	if err != nil {
		return fmt.Errorf("record deployment: %w", err)
	}
	return nil
}

func (s *Store) ListDeployments(ctx context.Context, appID int64) ([]*Deployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, image_ref, status, error, created_at FROM deployments WHERE app_id = $1 ORDER BY id`,
		appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []*Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.Status, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}
