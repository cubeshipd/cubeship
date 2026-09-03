package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/store"
)

type appResponse struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

func toAppResponse(a *store.App) appResponse {
	return appResponse{Name: a.Name, Domain: a.Domain, Image: a.Image, Status: a.Status}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Domain == "" {
		http.Error(w, "name and domain are required", http.StatusBadRequest)
		return
	}

	if _, err := s.store.GetAppByName(r.Context(), req.Name); err == nil {
		http.Error(w, "app already exists", http.StatusConflict)
		return
	}

	image := s.registryHost + "/" + req.Name
	app, err := s.store.CreateApp(r.Context(), req.Name, req.Domain, image)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, toAppResponse(app))
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		resp = append(resp, toAppResponse(a))
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
	writeJSON(w, http.StatusOK, toAppResponse(app))
}
