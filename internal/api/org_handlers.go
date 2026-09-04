package api

import (
	"encoding/json"
	"net/http"
)

type orgResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

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
