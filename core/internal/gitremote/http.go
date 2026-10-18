package gitremote

import (
	"context"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

type Deployer interface {
	DeployOnPushWithChangesAndIdentity(ctx context.Context, repo, branch, sha string, changed []string, complete bool) (int, error)
}

type Handler struct {
	svc    *Service
	deploy Deployer
}

func NewHandler(s *Service, d Deployer) *Handler { return &Handler{s, d} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /gitremotes", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /gitremotes", auth(http.HandlerFunc(h.connect)))
	r.Handle("DELETE /gitremotes/{id}", auth(http.HandlerFunc(h.disconnect)))
	r.Handle("GET /gitremotes/{id}/webhook", auth(http.HandlerFunc(h.webhookDetails)))
	r.Handle("GET /gitremotes/repositories", auth(http.HandlerFunc(h.repositories)))
	r.Handle("GET /gitremotes/branches", auth(http.HandlerFunc(h.branches)))
}

func (h *Handler) WebhookRoutes(r *httpx.Router) {
	r.HandleRootFunc("POST /hooks/gitremotes/{id}", h.webhook)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, database.ErrNotFound):
		http.Error(w, "connection not found", http.StatusNotFound)
	case errors.Is(err, ErrProvider), errors.Is(err, ErrURL), errors.Is(err, ErrToken), errors.Is(err, ErrRepo):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	connections, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, connections)
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Provider Provider `json:"provider"`
		BaseURL  string   `json:"base_url"`
		APIToken string   `json:"api_token"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input) != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	c, err := h.svc.Connect(r.Context(), user.FromContext(r.Context()), input.Provider, input.BaseURL, input.APIToken)
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, c)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid connection", http.StatusBadRequest)
		return
	}
	if err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), id); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) webhookDetails(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid connection", http.StatusBadRequest)
		return
	}
	secret, err := h.svc.WebhookSecret(r.Context(), user.FromContext(r.Context()), id)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"secret": secret})
}

func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	repositories, err := h.svc.Repositories(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, repositories)
}

func (h *Handler) branches(w http.ResponseWriter, r *http.Request) {
	branches, err := h.svc.Branches(r.Context(), user.FromContext(r.Context()), r.URL.Query().Get("repo"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, branches)
}

func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid connection", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	push, err := h.svc.Receive(r.Context(), id, body, r.Header)
	if err != nil {
		if errors.Is(err, ErrWebhook) {
			http.Error(w, "invalid delivery", http.StatusUnauthorized)
		} else {
			http.Error(w, "could not verify delivery", http.StatusServiceUnavailable)
		}
		return
	}
	if push != nil {
		if _, err := h.deploy.DeployOnPushWithChangesAndIdentity(r.Context(), push.RepositoryURL, push.Branch, push.SHA, push.Changed, push.Complete); err != nil {
			http.Error(w, "could not start deployment", http.StatusBadGateway)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}
