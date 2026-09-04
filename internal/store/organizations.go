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

const orgColumns = `id, slug, name, created_at`

func scanOrganization(row interface{ Scan(dest ...any) error }) (*Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (s *Store) CreateOrganization(ctx context.Context, slug, name string) (*Organization, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES ($1, $2) RETURNING `+orgColumns, slug, name)
	o, err := scanOrganization(row)
	if err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return o, nil
}

func (s *Store) GetOrganizationBySlug(ctx context.Context, slug string) (*Organization, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE slug = $1`, slug)
	o, err := scanOrganization(row)
	if err != nil {
		return nil, fmt.Errorf("get organization %q: %w", slug, err)
	}
	return o, nil
}

func (s *Store) GetOrganizationByID(ctx context.Context, id int64) (*Organization, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id)
	o, err := scanOrganization(row)
	if err != nil {
		return nil, fmt.Errorf("get organization %d: %w", id, err)
	}
	return o, nil
}

func (s *Store) ListOrganizations(ctx context.Context) ([]*Organization, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+orgColumns+` FROM organizations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []*Organization
	for rows.Next() {
		o, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}
