package api

import (
	"context"
	"net/http"

	"cubeship/internal/store"
)

// authorizeOrg reports whether user may act on orgID at at least
// minRole. Super-admins are always authorized. An org admin satisfies
// both RoleAdmin and RoleMember checks; a member only satisfies
// RoleMember. A nil user (unauthenticated) is never authorized.
//
// Takes user directly, rather than resolving it from an *http.Request,
// so both HTTP handlers (via authorizeOrgRequest) and MCP tool handlers
// (which have no *http.Request at all) share this one implementation.
func (s *Server) authorizeOrg(ctx context.Context, user *store.User, orgID int64, minRole store.Role) bool {
	if user == nil {
		return false
	}
	if user.IsSuperAdmin {
		return true
	}
	role, err := s.store.GetMembership(ctx, user.ID, orgID)
	if err != nil {
		return false
	}
	if minRole == store.RoleMember {
		return true
	}
	return role == store.RoleAdmin
}

// authorizeApp is authorizeOrg for an app's owning organization.
func (s *Server) authorizeApp(ctx context.Context, user *store.User, app *store.App, minRole store.Role) bool {
	return s.authorizeOrg(ctx, user, app.OrgID, minRole)
}

// authorizeOrgRequest is authorizeOrg for an HTTP handler, using the
// caller authMiddleware resolved into r's context.
func (s *Server) authorizeOrgRequest(r *http.Request, orgID int64, minRole store.Role) bool {
	return s.authorizeOrg(r.Context(), userFromContext(r.Context()), orgID, minRole)
}

// authorizeAppRequest is authorizeApp for an HTTP handler.
func (s *Server) authorizeAppRequest(r *http.Request, app *store.App, minRole store.Role) bool {
	return s.authorizeApp(r.Context(), userFromContext(r.Context()), app, minRole)
}
