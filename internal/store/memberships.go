package store

import (
	"context"
	"fmt"
)

type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type OrgMembership struct {
	OrgID   int64
	OrgSlug string
	OrgName string
	Role    Role
}

func addMembership(ctx context.Context, q queryer, userID, orgID int64, role Role) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO memberships (user_id, org_id, role) VALUES ($1, $2, $3)`,
		userID, orgID, string(role)); err != nil {
		return fmt.Errorf("add membership: %w", err)
	}
	return nil
}

func (s *Store) AddMembership(ctx context.Context, userID, orgID int64, role Role) error {
	return addMembership(ctx, s.db, userID, orgID, role)
}

// AddMembership is AddMembership inside t's transaction.
func (t *Tx) AddMembership(ctx context.Context, userID, orgID int64, role Role) error {
	return addMembership(ctx, t.q, userID, orgID, role)
}

func getMembership(ctx context.Context, q queryer, userID, orgID int64) (Role, error) {
	var role string
	err := q.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE user_id = $1 AND org_id = $2`, userID, orgID).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("get membership: %w", err)
	}
	return Role(role), nil
}

func (s *Store) GetMembership(ctx context.Context, userID, orgID int64) (Role, error) {
	return getMembership(ctx, s.db, userID, orgID)
}

// GetMembership is GetMembership inside t's transaction. A user with no
// membership in orgID comes back as an error wrapping ErrNotFound.
func (t *Tx) GetMembership(ctx context.Context, userID, orgID int64) (Role, error) {
	return getMembership(ctx, t.q, userID, orgID)
}

func (s *Store) ListMembershipsForUser(ctx context.Context, userID int64) ([]OrgMembership, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.org_id, o.slug, o.name, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1
		ORDER BY o.slug`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OrgMembership
	for rows.Next() {
		var m OrgMembership
		var role string
		if err := rows.Scan(&m.OrgID, &m.OrgSlug, &m.OrgName, &role); err != nil {
			return nil, err
		}
		m.Role = Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}
