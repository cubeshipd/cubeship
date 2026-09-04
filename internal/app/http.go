package app

import (
	"errors"
	"log"
	"net/http"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

// DefaultLogTail is how much of an app's log the API returns when the
// caller doesn't ask for a specific amount. A container that has been up
// for weeks holds far more output than anyone wants streamed at them, and
// the recent lines are the ones that explain what is happening now.
const DefaultLogTail = "500"

// Response is one app as both the API and the MCP tools report it.
type Response struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	Org         string `json:"org"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

func toResponse(a *Scoped) Response {
	return Response{
		Name: a.Name, Domain: a.Domain, Image: a.Image, Status: a.Status,
		Org: a.OrgSlug, Project: a.ProjectSlug, Environment: a.EnvironmentSlug,
	}
}

func toResponses(apps []*Scoped) []Response {
	out := make([]Response, 0, len(apps))
	for _, a := range apps {
		out = append(out, toResponse(a))
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("POST /apps", auth(http.HandlerFunc(h.create)))
	mux.Handle("GET /apps", auth(http.HandlerFunc(h.list)))
	mux.Handle("GET /apps/{name}", auth(http.HandlerFunc(h.get)))
	mux.Handle("POST /apps/{name}/deploy", auth(http.HandlerFunc(h.deploy)))
	mux.Handle("PUT /apps/{name}/env", auth(http.HandlerFunc(h.setEnv)))
	mux.Handle("GET /apps/{name}/logs", auth(http.HandlerFunc(h.logs)))
}

// WriteError maps this module's domain errors onto status codes, falling
// through to project's (and so to org's) for what it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoContainer):
		http.Error(w, "app has no running container yet", http.StatusConflict)
	default:
		project.WriteError(w, err)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Domain      string `json:"domain"`
		Org         string `json:"org"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil ||
		req.Name == "" || req.Domain == "" || req.Org == "" || req.Project == "" {
		http.Error(w, "name, domain, org and project are required", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()),
		req.Org, req.Project, req.Environment, req.Name, req.Domain)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	apps, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(apps))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), r.PathValue("name"), orgRoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a))
}

func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag string `json:"tag"`
	}
	// An empty or absent body is fine; Tag stays "" and defaults later.
	_ = httpx.DecodeJSON(r, &req)

	if _, err := h.svc.Deploy(r.Context(), user.FromContext(r.Context()), r.PathValue("name"), req.Tag); err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, err)
			return
		}
		// The app exists and the caller may deploy it; the deploy itself
		// failed, which is an upstream problem, not a bad request.
		httpx.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) setEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Vars envvar.Map `json:"vars"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.SetEnv(r.Context(), user.FromContext(r.Context()), r.PathValue("name"), req.Vars); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = DefaultLogTail
	}

	rc, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()), r.PathValue("name"), tail)
	if err != nil {
		WriteError(w, err)
		return
	}
	defer rc.Close()

	w.WriteHeader(http.StatusOK)
	// Containers are created without a TTY, so the Engine returns stdout
	// and stderr multiplexed behind an 8-byte binary frame header per
	// chunk. Copying that straight through prints binary garbage between
	// the log lines — demultiplex it first.
	if _, err := stdcopy.StdCopy(w, w, rc); err != nil {
		// The status line is already sent; all we can do is record it.
		log.Printf("logs for app %s: %v", r.PathValue("name"), err)
	}
}
