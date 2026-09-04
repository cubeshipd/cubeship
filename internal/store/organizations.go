package store

import (
	"context"
	"fmt"
	"time"
)

type Organization struct {
	ID        int64
	Slug      string
	Name      string
	CreatedAt time.Time
}

func (s *Store) CreateOrganization(ctx context.Context, slug, name string) (*Organization, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES (?, ?)`, slug, name); err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return s.GetOrganizationBySlug(ctx, slug)
}

func (s *Store) GetOrganizationBySlug(ctx context.Context, slug string) (*Organization, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at FROM organizations WHERE slug = ?`, slug)
	var o Organization
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
		return nil, fmt.Errorf("get organization %q: %w", slug, err)
	}
	return &o, nil
}

func (s *Store) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, created_at FROM organizations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*Organization
	for rows.Next() {
		var o Organization
		if err := rows.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, &o)
	}
	return orgs, rows.Err()
}
