package bitbucket

import (
	"context"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Deployer interface {
	DeployOnPush(ctx context.Context, repo, branch string) (int, error)
}
type Handler struct {
	svc    *Service
	deploy Deployer
}

func NewHandler(s *Service, d Deployer) *Handler { return &Handler{s, d} }
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /bitbucket", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /bitbucket", auth(http.HandlerFunc(h.connect)))
	r.Handle("DELETE /bitbucket/{id}", auth(http.HandlerFunc(h.disconnect)))
	r.Handle("GET /bitbucket/repositories", auth(http.HandlerFunc(h.repos)))
	r.Handle("GET /bitbucket/branches", auth(http.HandlerFunc(h.branches)))
}
func (h *Handler) WebhookRoutes(r *httpx.Router) {
	r.HandleRootFunc("POST /hooks/bitbucket", h.webhook)
}
func write(w http.ResponseWriter, e error) {
	if e == user.ErrUnauthenticated {
		http.Error(w, "unauthorized", 401)
	} else {
		http.Error(w, e.Error(), 409)
	}
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	v, e := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if e != nil {
		write(w, e)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	var q struct{ Code, State string }
	if json.NewDecoder(r.Body).Decode(&q) != nil {
		http.Error(w, "invalid body", 400)
		return
	}
	v, e := h.svc.Connect(r.Context(), user.FromContext(r.Context()), q.Code, q.State)
	if e != nil {
		write(w, e)
		return
	}
	v.accessToken = ""
	v.refreshToken = ""
	httpx.WriteJSON(w, 201, v)
}
func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e := h.svc.Disconnect(r.Context(), user.FromContext(r.Context()), id); e != nil {
		write(w, e)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) repos(w http.ResponseWriter, r *http.Request) {
	v, e := h.svc.Repositories(r.Context(), user.FromContext(r.Context()))
	if e != nil {
		write(w, e)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (h *Handler) branches(w http.ResponseWriter, r *http.Request) {
	v, e := h.svc.Branches(r.Context(), user.FromContext(r.Context()), r.URL.Query().Get("repo"))
	if e != nil {
		write(w, e)
		return
	}
	httpx.WriteJSON(w, 200, v)
}
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, e := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if e != nil {
		http.Error(w, "bad body", 400)
		return
	}
	repo, branches, e := h.svc.PushBranches(r.Context(), body, r.Header.Get("X-Hub-Signature"), r.Header.Get("X-Event-Key"))
	if e != nil {
		http.Error(w, "ignored", 200)
		return
	}
	for _, b := range branches {
		_, _ = h.deploy.DeployOnPush(r.Context(), strings.TrimPrefix(repo, "bitbucket.org/"), b)
	}
	w.WriteHeader(200)
}
