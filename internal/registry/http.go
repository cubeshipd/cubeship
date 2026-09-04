package registry

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"

	"cubeship/internal/app"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/regauth"
)

// scopeContext is context.Context; named so authorizeScope's signature
// reads as "the request's context" without importing context there.
type scopeContext = context.Context

// Routes registers the two endpoints the registry container itself calls.
// Neither sits behind the API's bearer-key middleware — the registry is
// not an API client — but neither is open; see each handler.
//
// Both are internal: `docker` and the registry container call them, no
// API consumer ever does, so they stay out of the OpenAPI document.
func (h *Handler) Routes(r *httpx.Router) {
	// Both stay at the root: these two addresses live in the registry
	// container's own configuration, not in anyone's client.
	r.HandleRootFunc("GET /v2/token", h.issueToken)
	r.HandleRootFunc("POST /hooks/registry", h.webhook)
}

// WaitForDeploys blocks until every deploy the daemon started has
// finished. Tests use it; the daemon does not.
func (h *Handler) WaitForDeploys() {
	h.apps.Orchestrator().Wait()
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
		// The repository a push landed in *is* the app's reference —
		// org/project/environment/app — so the notification resolves
		// without consulting a stored path.
		ref, err := app.ParseReference(ev.Target.Repository)
		if err != nil {
			continue // not a repository shaped like one of our apps
		}
		a, err := h.apps.Repo().ScopedByReference(r.Context(), ref.Org, ref.Project, ref.Environment, ref.Name)
		if err != nil {
			continue // no app owns this repository
		}
		// Start returns as soon as the deploy is recorded; the registry's
		// notification client gives up after 5s, and a real deploy takes
		// far longer. Which image that tag resolves to is the app
		// source's answer, not this handler's.
		if _, err := h.apps.Orchestrator().Start(r.Context(), a.ID, ev.Target.Tag); err != nil {
			log.Printf("registry webhook: could not start a deploy for %s: %v", a.Name, err)
		}
	}
	w.WriteHeader(http.StatusOK)
}
