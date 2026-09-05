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

const orgColumns = `id, slug, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanOrg(row scanner) (*Organization, error) {
	var o Organization
	if err := row.Scan(&o.ID, &o.Slug, &o.CreatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *Repository) Create(ctx context.Context, slug string) (*Organization, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO organizations (slug) VALUES ($1) RETURNING `+orgColumns, slug)
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
		SELECT m.org_id, o.slug, m.role
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
		if err := rows.Scan(&m.OrgID, &m.OrgSlug, &role); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Delete removes an organization, the projects and environments inside
// it, and every membership in it. The users themselves stay: they may
// belong to other organizations, and their API keys are their own.
//
// The apps must already be gone — their containers are not this
// package's to stop, which is what AppTeardown is for. The environments
// and projects are deleted from here rather than through internal/project
// because the rule is the organization's, and the dependency cannot run
// the other way.
func (r *Repository) Delete(ctx context.Context, orgID int64) error {
	if _, err := r.q.ExecContext(ctx,
		`DELETE FROM environments WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)`,
		orgID); err != nil {
		return fmt.Errorf("delete environments: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM projects WHERE org_id = $1`, orgID); err != nil {
		return fmt.Errorf("delete projects: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM memberships WHERE org_id = $1`, orgID); err != nil {
		return fmt.Errorf("delete memberships: %w", err)
	}
	if _, err := r.q.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		return fmt.Errorf("delete organization: %w", err)
	}
	return nil
}
