package api

import (
	"encoding/json"
	"log"
	"net/http"
)

type registryNotification struct {
	Events []struct {
		Action string `json:"action"`
		Target struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"target"`
	} `json:"events"`
}

func (s *Server) handleRegistryWebhook(w http.ResponseWriter, r *http.Request) {
	var payload registryNotification
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		// Malformed payload from a source we don't control: log and
		// still 200, there is nothing a retry would fix.
		log.Printf("registry webhook: invalid payload: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, ev := range payload.Events {
		if ev.Action != "push" || ev.Target.Tag == "" {
			continue
		}
		image := s.registryHost + "/" + ev.Target.Repository
		app, err := s.store.GetAppByImage(r.Context(), image)
		if err != nil {
			continue // no app tracks this repository
		}
		imageRef := image + ":" + ev.Target.Tag
		if err := s.orch.Deploy(r.Context(), app.Name, imageRef); err != nil {
			log.Printf("registry webhook: deploy failed for %s: %v", app.Name, err)
		}
	}
	w.WriteHeader(http.StatusOK)
}
