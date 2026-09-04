package api

import (
	"encoding/json"
	"net/http"
	"regexp"
)

type orgResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// orgSlugPattern is what a slug has to look like. The slug is not just a
// URL segment: it becomes a path component of every one of the org's
// registry image references (registry.<domain>/<slug>/<app>), and Docker
// rejects a repository path with uppercase letters, spaces or an extra
// "/" in it — a push against such an org could never work.
var orgSlugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func (s *Server) handleCreateOrg(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil || !user.IsSuperAdmin {
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
	if !orgSlugPattern.MatchString(req.Slug) {
		http.Error(w, "slug must be lowercase letters, digits and dashes, starting and ending with a letter or digit", http.StatusBadRequest)
		return
	}
	if _, err := s.store.GetOrganizationBySlug(r.Context(), req.Slug); err == nil {
		http.Error(w, "organization already exists", http.StatusConflict)
		return
	}

	org, err := s.store.CreateOrganization(r.Context(), req.Slug, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, orgResponse{Slug: org.Slug, Name: org.Name})
}

func (s *Server) handleListOrgs(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if user.IsSuperAdmin {
		orgs, err := s.store.ListOrganizations(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := make([]orgResponse, 0, len(orgs))
		for _, o := range orgs {
			resp = append(resp, orgResponse{Slug: o.Slug, Name: o.Name})
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	memberships, err := s.store.ListMembershipsForUser(r.Context(), user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]orgResponse, 0, len(memberships))
	for _, m := range memberships {
		resp = append(resp, orgResponse{Slug: m.OrgSlug, Name: m.OrgName})
	}
	writeJSON(w, http.StatusOK, resp)
}
