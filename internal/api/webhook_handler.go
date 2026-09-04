package api

import (
	"context"
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

// handleRegistryWebhook receives push notifications from the embedded
// registry.
//
// It is not behind authMiddleware because the registry is not an API
// client, but it is not open either: the registry's config.yml sends a
// static Authorization header carrying the daemon's token (see
// bootstrap.RegistryConfigYAML). Without that check, any internet host
// that can reach :9000 could forge a push notification and force a
// redeploy of any tracked app to any tag in the registry.
//
// The deploy itself runs in the background against a fresh context. The
// registry's notification client gives up after 5s and retries up to 5
// times; a real deploy (pull, create, start, health-check polling)
// routinely takes longer than that, so doing the work inline would
// cancel the request context mid-deploy and trigger a retry storm.
func (s *Server) handleRegistryWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

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
		// The stored Image column holds the public push path, which is
		// what the notification's repository name maps to.
		image := s.registryHost + "/" + ev.Target.Repository
		app, err := s.store.GetAppByImage(r.Context(), image)
		if err != nil {
			continue // no app tracks this repository
		}
		// ...but the daemon pulls over loopback.
		pullRef := localPullRef(image, ev.Target.Tag)
		s.deployInBackground(app.Name, pullRef)
	}
	w.WriteHeader(http.StatusOK)
}

// deployInBackground runs a deploy detached from the request that
// triggered it, so the caller's timeout can't cancel it.
func (s *Server) deployInBackground(appName, pullRef string) {
	s.deployWG.Add(1)
	go func() {
		defer s.deployWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), webhookDeployTimeout)
		defer cancel()
		if err := s.orch.Deploy(ctx, appName, pullRef); err != nil {
			log.Printf("registry webhook: deploy failed for %s: %v", appName, err)
		}
	}()
}
