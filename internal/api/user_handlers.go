package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

func (s *Server) handleCreateOrgUser(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	org, err := s.store.GetOrganizationBySlug(r.Context(), slug)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleAdmin) {
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

	user, err := s.store.CreateUser(r.Context(), req.Username, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.store.AddMembership(r.Context(), user.ID, org.ID, role); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	key, err := authkey.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateAPIKey(r.Context(), user.ID, authkey.Hash(key)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"username": user.Username,
		"org":      org.Slug,
		"role":     string(role),
		"api_key":  key,
	})
}

func (s *Server) handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := s.store.RevokeAPIKeysForUser(r.Context(), user.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	key, err := authkey.Generate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := s.store.CreateAPIKey(r.Context(), user.ID, authkey.Hash(key)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
}
