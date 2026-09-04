package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"cubeship/internal/deploy"
	"cubeship/internal/store"

	"github.com/docker/docker/pkg/stdcopy"
)

func (s *Server) handleManualDeploy(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Tag string `json:"tag"`
	}
	json.NewDecoder(r.Body).Decode(&req) // empty/absent body is fine, Tag stays ""
	if req.Tag == "" {
		req.Tag = "latest"
	}

	// app.Image is the public push path (registry.<domain>/<app>); the
	// daemon pulls the same repository over loopback instead.
	pullRef := localPullRef(app.Image, req.Tag)
	if err := s.orch.Deploy(r.Context(), app.Name, pullRef); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSetEnv(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Vars map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := s.store.SetAppEnv(r.Context(), app.ID, req.Vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
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

	rc, err := s.orch.Logs(r.Context(), name, "")
	if errors.Is(err, deploy.ErrNoContainer) {
		http.Error(w, "app has no running container yet", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.WriteHeader(http.StatusOK)
	// Containers are created without a TTY, so the Engine returns
	// stdout and stderr multiplexed behind an 8-byte binary frame
	// header per chunk. Copying that straight through prints binary
	// garbage between the log lines — demultiplex it first.
	if _, err := stdcopy.StdCopy(w, w, rc); err != nil {
		// The status line is already sent; all we can do is record it.
		log.Printf("logs for app %s: %v", name, err)
	}
}
