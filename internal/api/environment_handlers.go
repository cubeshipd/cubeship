package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"cubeship/internal/store"
)

type environmentResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRequest(w, r, store.RoleAdmin)
	if !ok {
		return
	}

	var req struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Slug == "" || req.Name == "" {
		http.Error(w, "slug and name are required", http.StatusBadRequest)
		return
	}
	if !slugPattern.MatchString(req.Slug) {
		http.Error(w, "slug must be lowercase letters, digits and dashes, starting and ending with a letter or digit", http.StatusBadRequest)
		return
	}
	if _, err := s.store.GetEnvironmentBySlug(r.Context(), project.ID, req.Slug); err == nil {
		http.Error(w, "environment already exists", http.StatusConflict)
		return
	}

	env, err := s.store.CreateEnvironment(r.Context(), project.ID, req.Slug, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, environmentResponse{Slug: env.Slug, Name: env.Name})
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRequest(w, r, store.RoleMember)
	if !ok {
		return
	}

	envs, err := s.store.ListEnvironmentsForProject(r.Context(), project.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]environmentResponse, 0, len(envs))
	for _, e := range envs {
		resp = append(resp, environmentResponse{Slug: e.Slug, Name: e.Name})
	}
	writeJSON(w, http.StatusOK, resp)
}

// environmentFromRequest resolves {orgSlug}/{projectSlug}/{envSlug} and
// checks the caller is authorized at minRole in that org, folding every
// failure into one 404 — see projectFromRequest.
func (s *Server) environmentFromRequest(w http.ResponseWriter, r *http.Request, minRole store.Role) (*store.Environment, bool) {
	project, ok := s.projectFromRequest(w, r, minRole)
	if !ok {
		return nil, false
	}
	env, err := s.store.GetEnvironmentBySlug(r.Context(), project.ID, r.PathValue("envSlug"))
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return nil, false
	}
	return env, true
}

func (s *Server) handleSetEnvironmentEnv(w http.ResponseWriter, r *http.Request) {
	env, ok := s.environmentFromRequest(w, r, store.RoleAdmin)
	if !ok {
		return
	}

	var req struct {
		Vars map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := s.store.SetEnvironmentEnv(r.Context(), env.ID, req.Vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// errProductionEnvUndeletable and errEnvironmentHasApps are the two
// reasons handleDeleteEnvironment refuses a delete.
var errProductionEnvUndeletable = errors.New(`the "production" environment can never be deleted`)
var errEnvironmentHasApps = errors.New("environment still has apps in it")

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	env, ok := s.environmentFromRequest(w, r, store.RoleAdmin)
	if !ok {
		return
	}
	if env.Slug == store.ProductionEnvSlug {
		http.Error(w, errProductionEnvUndeletable.Error(), http.StatusForbidden)
		return
	}
	count, err := s.store.CountAppsInEnvironment(r.Context(), env.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count > 0 {
		http.Error(w, errEnvironmentHasApps.Error(), http.StatusConflict)
		return
	}
	if err := s.store.DeleteEnvironment(r.Context(), env.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
