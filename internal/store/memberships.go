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

func (s *Store) AddMembership(ctx context.Context, userID, orgID int64, role Role) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO memberships (user_id, org_id, role) VALUES (?, ?, ?)`,
		userID, orgID, string(role)); err != nil {
		return fmt.Errorf("add membership: %w", err)
	}
	return nil
}

func (s *Store) GetMembership(ctx context.Context, userID, orgID int64) (Role, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE user_id = ? AND org_id = ?`, userID, orgID).Scan(&role)
	if err != nil {
		return "", fmt.Errorf("get membership: %w", err)
	}
	return Role(role), nil
}

func (s *Store) ListMembershipsForUser(ctx context.Context, userID int64) ([]OrgMembership, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.org_id, o.slug, o.name, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = ?
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
