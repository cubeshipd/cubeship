package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/store"
)

type projectResponse struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Environments []string `json:"environments,omitempty"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	org, err := s.store.GetOrganizationBySlug(r.Context(), r.PathValue("orgSlug"))
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleAdmin) {
		http.Error(w, "forbidden", http.StatusForbidden)
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
	if _, err := s.store.GetProjectBySlug(r.Context(), org.ID, req.Slug); err == nil {
		http.Error(w, "project already exists", http.StatusConflict)
		return
	}

	project, env, err := s.store.CreateProjectWithDefaultEnvironment(r.Context(), org.ID, req.Slug, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, projectResponse{Slug: project.Slug, Name: project.Name, Environments: []string{env.Slug}})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	org, err := s.store.GetOrganizationBySlug(r.Context(), r.PathValue("orgSlug"))
	if err != nil {
		http.Error(w, "organization not found", http.StatusNotFound)
		return
	}
	if !s.authorizeOrg(r, org.ID, store.RoleMember) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	projects, err := s.store.ListProjectsForOrg(r.Context(), org.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]projectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, projectResponse{Slug: p.Slug, Name: p.Name})
	}
	writeJSON(w, http.StatusOK, resp)
}

// projectFromRequest resolves {orgSlug}/{projectSlug} from the request
// path and checks the caller is authorized at minRole in that org,
// folding every failure into one 404 — like handleGetApp, this acts on a
// single already-existing resource, so an outsider probing a slug learns
// nothing from the response.
func (s *Server) projectFromRequest(w http.ResponseWriter, r *http.Request, minRole store.Role) (*store.Project, bool) {
	org, err := s.store.GetOrganizationBySlug(r.Context(), r.PathValue("orgSlug"))
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return nil, false
	}
	if !s.authorizeOrg(r, org.ID, minRole) {
		http.Error(w, "project not found", http.StatusNotFound)
		return nil, false
	}
	project, err := s.store.GetProjectBySlug(r.Context(), org.ID, r.PathValue("projectSlug"))
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return nil, false
	}
	return project, true
}

func (s *Server) handleSetProjectEnv(w http.ResponseWriter, r *http.Request) {
	project, ok := s.projectFromRequest(w, r, store.RoleAdmin)
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
	if err := s.store.SetProjectEnv(r.Context(), project.ID, req.Vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
