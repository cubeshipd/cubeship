package registry

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/regauth"
)

// scopeContext is context.Context; named so authorizeScope's signature
// reads as "the request's context" without importing context there.
type scopeContext = context.Context

// webhookDeployTimeout bounds a deploy kicked off by a registry push. The
// webhook acks immediately, so this is not the registry's notification
// timeout — it just stops a wedged deploy running forever.
const webhookDeployTimeout = 10 * time.Minute

// Routes registers the two endpoints the registry container itself calls.
// Neither sits behind the API's bearer-key middleware — the registry is
// not an API client — but neither is open; see each handler.
//
// Both are internal: `docker` and the registry container call them, no
// API consumer ever does, so they stay out of the OpenAPI document.
func (h *Handler) Routes(r *httpx.Router) {
	r.HandleInternalFunc("GET /v2/token", h.issueToken)
	r.HandleInternalFunc("POST /hooks/registry", h.webhook)
}

// WaitForDeploys blocks until every webhook-triggered deploy has
// finished. Tests use it; the daemon does not.
func (h *Handler) WaitForDeploys() {
	h.deployWG.Wait()
}

// issueToken implements the realm the registry's config.yml points at:
// docker login/push/pull exchange the caller's username and API key
// (HTTP Basic auth) plus a requested scope for a short-lived JWT scoped
// to exactly what that user's organization membership authorizes.
//
// This route is deliberately NOT behind the bearer-key middleware —
// Basic auth is what the registry sends here — but it is not open
// either: every request must still resolve to a real user via their API
// key before any token is issued.
func (h *Handler) issueToken(w http.ResponseWriter, r *http.Request) {
	if h.signingKey == nil {
		http.Error(w, "registry token signing not configured", http.StatusServiceUnavailable)
		return
	}

	username, key, ok := r.BasicAuth()
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	caller, _, err := h.users.Authenticate(r.Context(), key)
	if err != nil || caller.Username != username {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var access []regauth.AccessEntry
	for _, scope := range r.URL.Query()["scope"] {
		access = append(access, h.authorizeScope(r.Context(), caller, scope)...)
	}

	token, err := regauth.IssueToken(h.signingKey, regauth.TokenIssuer, regauth.TokenService, caller.Username, access)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"token":        token,
		"access_token": token,
		"expires_in":   int(regauth.TokenTTL.Seconds()),
	})
}

type notification struct {
	Events []struct {
		Action string `json:"action"`
		Target struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"target"`
	} `json:"events"`
}

// webhook receives push notifications from the embedded registry.
//
// It is not behind the API's authentication because the registry is not
// an API client, but it is not open either: the registry's config.yml
// sends a static Authorization header carrying the daemon's token.
// Without that check, any internet host that can reach the daemon's port
// could forge a push notification and force a redeploy of any tracked app
// to any tag in the registry.
//
// The deploy itself runs in the background against a fresh context. The
// registry's notification client gives up after 5s and retries up to 5
// times; a real deploy routinely takes longer than that, so doing the
// work inline would cancel the request context mid-deploy and trigger a
// retry storm.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	// Constant-time: the token is a fixed secret compared on every
	// notification, which is exactly the shape a timing attack needs.
	got := r.Header.Get("Authorization")
	want := "Bearer " + h.webhookToken
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload notification
	if err := httpx.DecodeJSON(r, &payload); err != nil {
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
		// The stored image column holds the public push path, which is
		// what the notification's repository name maps to.
		image := h.registryHost + "/" + ev.Target.Repository
		a, err := h.apps.Repo().ByImage(r.Context(), image)
		if err != nil {
			continue // no app tracks this repository
		}
		// ...but the daemon pulls over loopback.
		h.deployInBackground(a.ID, a.Name, app.LocalPullRef(image, ev.Target.Tag))
	}
	w.WriteHeader(http.StatusOK)
}

// deployInBackground runs a deploy detached from the request that
// triggered it, so the caller's timeout can't cancel it.
func (h *Handler) deployInBackground(appID int64, appName, pullRef string) {
	h.deployWG.Add(1)
	go func() {
		defer h.deployWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), webhookDeployTimeout)
		defer cancel()
		if err := h.apps.Orchestrator().Deploy(ctx, appID, pullRef); err != nil {
			log.Printf("registry webhook: deploy failed for %s: %v", appName, err)
		}
	}()
}
