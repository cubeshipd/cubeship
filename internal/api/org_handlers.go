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

// slugPattern is what every slug in the API — organization, project,
// environment, and app name — has to look like: kebab-case, no accents
// or other special characters. Org and app name both become path
// components of the app's registry image reference
// (registry.<domain>/<org-slug>/<app-name>), and Docker rejects a
// repository path with uppercase letters, accents, spaces or an extra
// "/" in it — a push against such a name could never work. Project and
// environment slugs carry no such constraint from Docker, but are held
// to the same shape for consistency and because they appear verbatim in
// URLs.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

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
	if !slugPattern.MatchString(req.Slug) {
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
