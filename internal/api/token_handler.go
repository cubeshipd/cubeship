package api

import (
	"net/http"
	"strings"

	"cubeship/internal/authkey"
	"cubeship/internal/regauth"
	"cubeship/internal/store"
)

// handleRegistryToken implements the realm the registry's config.yml
// points at (see bootstrap.RegistryConfigYAML's auth.token section):
// docker login/push/pull exchange the caller's username + API key
// (HTTP Basic auth) plus a requested scope for a short-lived JWT scoped
// to exactly what that user's organization membership authorizes.
//
// This route is deliberately NOT behind authMiddleware — Basic auth,
// not a bearer API key, is what the registry sends here — but it is
// not open either: every request must still resolve to a real user via
// their API key before any token is issued.
func (s *Server) handleRegistryToken(w http.ResponseWriter, r *http.Request) {
	if s.registrySigningKey == nil {
		http.Error(w, "registry token signing not configured", http.StatusServiceUnavailable)
		return
	}

	username, key, ok := r.BasicAuth()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := s.store.GetUserByAPIKeyHash(r.Context(), authkey.Hash(key))
	if err != nil || user.Username != username {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var access []regauth.AccessEntry
	for _, scope := range r.URL.Query()["scope"] {
		access = append(access, s.authorizeRegistryScope(r, user, scope)...)
	}

	token, err := regauth.IssueToken(s.registrySigningKey, regauth.TokenIssuer, regauth.TokenService, user.Username, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":        token,
		"access_token": token,
		"expires_in":   int(regauth.TokenTTL.Seconds()),
	})
}

// authorizeRegistryScope parses one "type:name:actions" scope string
// (the shape the Docker client sends, e.g.
// "repository:acme/myapp:pull,push") and returns it back with only the
// actions the caller's org membership actually grants for it. An action
// the caller isn't authorized for is silently dropped from the result
// rather than failing the whole request — an empty/omitted access entry
// for a scope is exactly how the token spec expects "denied" to be
// expressed; the registry itself then rejects that specific action.
func (s *Server) authorizeRegistryScope(r *http.Request, user *store.User, scope string) []regauth.AccessEntry {
	parts := strings.SplitN(scope, ":", 3)
	if len(parts) != 3 {
		return nil
	}
	typ, name, actionsStr := parts[0], parts[1], parts[2]
	if typ != "repository" {
		return nil
	}

	orgSlug := name
	if i := strings.Index(name, "/"); i >= 0 {
		orgSlug = name[:i]
	}

	if !user.IsSuperAdmin {
		org, err := s.store.GetOrganizationBySlug(r.Context(), orgSlug)
		if err != nil {
			return nil
		}
		if _, err := s.store.GetMembership(r.Context(), user.ID, org.ID); err != nil {
			return nil
		}
	}

	return []regauth.AccessEntry{{Type: typ, Name: name, Actions: strings.Split(actionsStr, ",")}}
}
