package api

import (
	"net/http"

	"cubeship/internal/store"
)

// authorizeOrg reports whether the caller authenticated by
// authMiddleware may act on orgID at at least minRole. Super-admins are
// always authorized. An org admin satisfies both RoleAdmin and
// RoleMember checks; a member only satisfies RoleMember.
func (s *Server) authorizeOrg(r *http.Request, orgID int64, minRole store.Role) bool {
	user := userFromContext(r.Context())
	if user == nil {
		return false
	}
	if user.IsSuperAdmin {
		return true
	}
	role, err := s.store.GetMembership(r.Context(), user.ID, orgID)
	if err != nil {
		return false
	}
	if minRole == store.RoleMember {
		return true
	}
	return role == store.RoleAdmin
}

// authorizeApp is authorizeOrg for an app's owning organization.
func (s *Server) authorizeApp(r *http.Request, app *store.App, minRole store.Role) bool {
	return s.authorizeOrg(r, app.OrgID, minRole)
}
