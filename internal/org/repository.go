package org

import (
	"context"
	"fmt"

	"cubeship/internal/platform/database"
)

// Repository reads and writes organizations and memberships.
type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

const orgColumns = `id, slug, name, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanOrg(row scanner) (*Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Slug, &o.Name, &o.CreatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repository) Create(ctx context.Context, slug, name string) (*Organization, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES ($1, $2) RETURNING `+orgColumns, slug, name)
	o, err := scanOrg(row)
	if err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return o, nil
}

func (r *Repository) BySlug(ctx context.Context, slug string) (*Organization, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE slug = $1`, slug)
	o, err := scanOrg(row)
	if err != nil {
		return nil, fmt.Errorf("get organization %q: %w", slug, err)
	}
	return o, nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Organization, error) {
	row := r.q.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM organizations WHERE id = $1`, id)
	o, err := scanOrg(row)
	if err != nil {
		return nil, fmt.Errorf("get organization %d: %w", id, err)
	}
	return o, nil
}

func (r *Repository) List(ctx context.Context) ([]*Organization, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+orgColumns+` FROM organizations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Organization
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Repository) AddMembership(ctx context.Context, userID, orgID int64, role Role) error {
	if _, err := r.q.ExecContext(ctx,
		`INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, $3)`,
		userID, orgID, string(role)); err != nil {
		return fmt.Errorf("add membership: %w", err)
	}
	return nil
}

// MembershipRole returns the role userID holds in orgID, or an error
// wrapping database.ErrNotFound when they hold none.
func (r *Repository) MembershipRole(ctx context.Context, userID, orgID int64) (Role, error) {
	var role string
	err := r.q.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE user_id = $1 AND org_id = $2`, userID, orgID).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("get membership: %w", err)
	}
	return Role(role), nil
}

func (r *Repository) ListMembershipsForUser(ctx context.Context, userID int64) ([]Membership, error) {
	rows, err := r.q.QueryContext(ctx, `
		SELECT m.org_id, o.slug, o.name, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY o.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Membership
	for rows.Next() {
		var m Membership
		var role string
		if err := rows.Scan(&m.OrgID, &m.OrgSlug, &m.OrgName, &role); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}
