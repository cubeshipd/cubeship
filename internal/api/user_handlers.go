package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/store"
)

type createOrgUserResponse struct {
	Username string `json:"username"`
	Org      string `json:"org"`
	Role     string `json:"role"`
	// APIKey is the new user's key, shown exactly once. It is omitted
	// when an existing user was added to a further organization — that
	// user keeps the key they already have.
	APIKey string `json:"api_key,omitempty"`
}

// handleCreateOrgUser adds a user to an organization, creating them if
// this is their first one. A username that already exists gains a
// membership rather than colliding on the unique index: users belong to
// as many organizations as they are added to, each with its own role.
func (s *Server) handleCreateOrgUser(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	org, err := s.store.GetOrganizationBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrgRequest(r, org.ID, store.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	role := store.Role(req.Role)
	if role != store.RoleAdmin && role != store.RoleMember {
		http.Error(w, "role must be \"admin\" or \"member\"", http.StatusBadRequest)
		return
	}

	apiKey, err := s.addOrgUser(r.Context(), org, req.Username, role)
	if errors.Is(err, errAlreadyMember) {
		http.Error(w, errAlreadyMember.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, createOrgUserResponse{Username: req.Username, Org: org.Slug, Role: string(role), APIKey: apiKey})
}

// handleRotateAPIKey replaces exactly the key this request authenticated
// with, keeping its name — every OTHER key the caller holds (an "mcp"
// key created via handleCreateAPIKey, say) is left alone. A user can
// hold several independent keys precisely so that rotating one — routine
// hygiene on the key your terminal uses, for instance — can't silently
// invalidate an unrelated integration's key.
func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	keyHash := apiKeyHashFromContext(r.Context())
	if user == nil || keyHash == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	key, err := s.rotateAPIKey(r.Context(), user, keyHash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}

type apiKeyResponse struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CurrentKey bool       `json:"current_key"`
}

// handleCreateAPIKey issues an ADDITIONAL API key for the caller,
// independent of any key they already hold — this is how an MCP client
// like Claude Code gets its own credential, separate from the one your
// terminal uses, so revoking or rotating one never touches the other.
func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	created, generated, err := s.createAdditionalAPIKey(r.Context(), user, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      created.ID,
		"name":    created.Name,
		"api_key": generated,
	})
}

// handleListAPIKeys lists metadata for every key the caller holds. The
// key values themselves are never shown again after creation — only the
// id, name and usage timestamps, enough to tell which is which and
// recognize one that's gone stale.
func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	keyHash := apiKeyHashFromContext(r.Context())

	keys, err := s.store.ListAPIKeysForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]apiKeyResponse, 0, len(keys))
	for _, k := range keys {
		resp = append(resp, apiKeyResponse{
			ID: k.ID, Name: k.Name, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt,
			CurrentKey: k.KeyHash == keyHash,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRevokeAPIKey revokes one of the caller's own keys by id — an
// integration being decommissioned, say — without touching any other key
// they hold.
func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	if err := s.revokeAPIKey(r.Context(), user, id); err != nil {
		switch {
		case errors.Is(err, errLastAPIKey):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "api key not found", http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleWhoAmI reports the identity of the caller's own API key. The
// CLI's `registry login` uses this to learn the username to
// authenticate the registry's per-user token auth with — the saved
// credentials file only ever stored the key itself, never the
// username.
func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"username":       user.Username,
		"is_super_admin": user.IsSuperAdmin,
	})
}
