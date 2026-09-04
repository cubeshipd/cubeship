package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cubeship/internal/org"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// Response is one installation as the API returns it.
type Response struct {
	ID             int64     `json:"id"`
	InstallationID int64     `json:"installation_id"`
	Account        string    `json:"account"`
	CreatedAt      time.Time `json:"created_at"`
}

func toResponse(i *Installation) Response {
	return Response{ID: i.ID, InstallationID: i.GitHubID, Account: i.Account, CreatedAt: i.CreatedAt}
}

func toResponses(installations []*Installation) []Response {
	out := make([]Response, 0, len(installations))
	for _, i := range installations {
		out = append(out, toResponse(i))
	}
	return out
}

// Deployer is what a push turns into. The github module knows nothing
// about apps beyond this.
type Deployer interface {
	// DeployOnPush starts a deploy for every app that builds from this
	// repository at this branch, and reports how many it started.
	DeployOnPush(ctx context.Context, orgID int64, repo, branch string) (int, error)
}

type Handler struct {
	svc    *Service
	deploy Deployer

	// deploys tracks what a webhook set going, so a test can wait for
	// it. The daemon does not.
	deploys sync.WaitGroup
}

func NewHandler(svc *Service, deploy Deployer) *Handler {
	return &Handler{svc: svc, deploy: deploy}
}

// WaitForDeploys blocks until every deploy this handler started has
// finished. For tests.
func (h *Handler) WaitForDeploys() { h.deploys.Wait() }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /orgs/{orgSlug}/github", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /orgs/{orgSlug}/github", auth(http.HandlerFunc(h.connect)))
	r.Handle("DELETE /orgs/{orgSlug}/github/{id}", auth(http.HandlerFunc(h.disconnect)))
	r.Handle("GET /orgs/{orgSlug}/github/repositories", auth(http.HandlerFunc(h.repositories)))
	r.Handle("GET /orgs/{orgSlug}/github/branches", auth(http.HandlerFunc(h.branches)))
	// Instance configuration rather than an organization's: this is how
	// the instance becomes a GitHub App at all.
	r.Handle("POST /settings/github/manifest", auth(http.HandlerFunc(h.registerFromManifest)))
}

// WebhookRoutes mounts the one endpoint GitHub itself calls. It stays at
// the root, outside the API prefix, because its address is written into
// the App's registration rather than into anyone's client.
func (h *Handler) WebhookRoutes(r *httpx.Router) {
	r.HandleRootFunc("POST /hooks/github", h.webhook)
}

func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, org.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, org.ErrNotFound), errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrNotConfigured), errors.Is(err, ErrNoInstallation):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNotGranted):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, settings.ErrSuperAdminOnly):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	installations, err := h.svc.List(ctx, user.FromContext(ctx), r.PathValue("orgSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"installations": toResponses(installations),
		// Where to send someone to install the App. Empty until this
		// instance is registered as one.
		"install_url": h.svc.InstallURL(ctx),
	})
}

// repositories is what the dashboard offers instead of a URL field.
// Picking from a list cannot mistype an owner, and cannot name a
// repository this instance has no way to clone.
func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	repos, err := h.svc.Repositories(ctx, user.FromContext(ctx), r.PathValue("orgSlug"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, repos)
}

// branches lists one repository's branches, named as owner/name in the
// query. Same reason: a branch is chosen, not spelled.
func (h *Handler) branches(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		http.Error(w, "name the repository with ?repo=owner/name", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	branches, err := h.svc.Branches(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), repo)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, branches)
}

// registerFromManifest finishes the flow that spares someone creating
// an App by hand: GitHub redirects back with a code, and this exchanges
// it for the App it just made.
func (h *Handler) registerFromManifest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Code == "" {
		http.Error(w, "code is required", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	values, err := h.svc.RegisterFromManifest(ctx, user.FromContext(ctx), req.Code)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settings.ToResponse(values))
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstallationID int64  `json:"installation_id"`
		Account        string `json:"account"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	created, err := h.svc.Connect(ctx, user.FromContext(ctx), r.PathValue("orgSlug"),
		req.InstallationID, req.Account)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.Disconnect(ctx, user.FromContext(ctx), r.PathValue("orgSlug"), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxWebhookBody bounds what will be read from a delivery. GitHub's
// payloads are well under this; the limit is there so an endpoint that
// has not been authenticated yet cannot be used to exhaust memory.
const maxWebhookBody = 8 << 20

// pushEvent is the part of GitHub's push payload that matters here.
type pushEvent struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// installationEvent is how GitHub says an App was uninstalled.
type installationEvent struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// webhook is what makes a push a deploy.
//
// Every failure that is not a forged signature answers 200: GitHub
// retries a delivery it could not deliver, and a payload this daemon
// cannot act on is not something a retry would fix.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "could not read the delivery", http.StatusBadRequest)
		return
	}

	// The signature is over the exact bytes sent, so this happens before
	// anything is decoded — and before any work is done on their behalf.
	if err := h.svc.VerifyWebhook(r.Context(), body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "push":
		h.handlePush(r, body)
	case "installation":
		h.handleInstallation(r, body)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handlePush(r *http.Request, body []byte) {
	var event pushEvent
	if err := json.Unmarshal(body, &event); err != nil {
		log.Printf("github webhook: invalid push payload: %v", err)
		return
	}
	branch, ok := BranchOf(event.Ref)
	if !ok {
		return // a tag, or a branch being deleted
	}
	if event.Repository.FullName == "" || event.Installation.ID == 0 {
		return
	}

	// The installation is what says whose repository this is. Trusting
	// the payload's repository name alone would let anyone who can forge
	// a delivery deploy somebody else's app — which the signature
	// already prevents, but the ownership check is what makes it true
	// rather than merely unlikely.
	installation, found, err := h.svc.Repo().ByGitHubID(r.Context(), event.Installation.ID)
	if err != nil || !found {
		return
	}

	h.deploys.Add(1)
	go func() {
		defer h.deploys.Done()
		// Its own context: the delivery's is cancelled the moment this
		// handler answers, and a deploy outlives that by design.
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		started, err := h.deploy.DeployOnPush(ctx, installation.OrgID, event.Repository.FullName, branch)
		if err != nil {
			log.Printf("github webhook: could not deploy %s@%s: %v",
				event.Repository.FullName, branch, err)
			return
		}
		if started > 0 {
			log.Printf("github webhook: %s@%s started %d deploy(s)",
				event.Repository.FullName, branch, started)
		}
	}()
}

// handleInstallation keeps the record honest when someone uninstalls the
// App. A grant that no longer exists must stop being offered to clones,
// which would otherwise fail with a token GitHub has already revoked.
func (h *Handler) handleInstallation(r *http.Request, body []byte) {
	var event installationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return
	}
	if event.Action != "deleted" || event.Installation.ID == 0 {
		return
	}
	if err := h.svc.Repo().DeleteByGitHubID(r.Context(), event.Installation.ID); err != nil {
		log.Printf("github webhook: could not forget installation %d: %v", event.Installation.ID, err)
	}
}
