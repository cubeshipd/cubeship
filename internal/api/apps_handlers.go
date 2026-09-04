package api

import (
	"context"
	"encoding/json"
	"net/http"

	"cubeship/internal/store"
)

type appResponse struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

// toAppResponse looks up a's project and environment to include their
// slugs in the response — apps live in an environment now, so callers
// need to see which one without a separate request.
func (s *Server) toAppResponse(ctx context.Context, a *store.App) (appResponse, error) {
	resp := appResponse{Name: a.Name, Domain: a.Domain, Image: a.Image, Status: a.Status}
	project, err := s.store.GetProjectByID(ctx, a.ProjectID)
	if err != nil {
		return appResponse{}, err
	}
	env, err := s.store.GetEnvironmentByID(ctx, a.EnvironmentID)
	if err != nil {
		return appResponse{}, err
	}
	resp.Project = project.Slug
	resp.Environment = env.Slug
	return resp, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Domain      string `json:"domain"`
		Org         string `json:"org"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Domain == "" || req.Org == "" || req.Project == "" {
		http.Error(w, "name, domain, org and project are required", http.StatusBadRequest)
		return
	}
	if req.Environment == "" {
		req.Environment = store.ProductionEnvSlug
	}
	// The name becomes a path component of the app's registry image
	// reference (registry.<domain>/<org>/<name>) — see slugPattern.
	if !slugPattern.MatchString(req.Name) {
		http.Error(w, "name must be lowercase letters, digits and dashes, starting and ending with a letter or digit", http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrganizationBySlug(r.Context(), req.Org)
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrgRequest(r, org.ID, store.RoleMember) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	project, err := s.store.GetProjectBySlug(r.Context(), org.ID, req.Project)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	env, err := s.store.GetEnvironmentBySlug(r.Context(), project.ID, req.Environment)
	if err != nil {
		http.Error(w, "environment not found", http.StatusNotFound)
		return
	}

	if _, err := s.store.GetAppByName(r.Context(), req.Name); err == nil {
		http.Error(w, "app already exists", http.StatusConflict)
		return
	}

	image := s.registryHost + "/" + req.Org + "/" + req.Name
	app, err := s.store.CreateApp(r.Context(), org.ID, project.ID, env.ID, req.Name, req.Domain, image)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := s.toAppResponse(r.Context(), app)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		if !s.authorizeAppRequest(r, a, store.RoleMember) {
			continue
		}
		ar, err := s.toAppResponse(r.Context(), a)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp = append(resp, ar)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	app, err := s.store.GetAppByName(r.Context(), name)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	if !s.authorizeAppRequest(r, app, store.RoleMember) {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	resp, err := s.toAppResponse(r.Context(), app)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
