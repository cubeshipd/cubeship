package templateinstall

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// RunResponse is one run as the API returns it.
type RunResponse struct {
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`
	FromRelease string     `json:"from_release,omitempty"`
	ToRelease   string     `json:"to_release,omitempty"`
	Status      string     `json:"status"`
	Step        string     `json:"step,omitempty"`
	Error       string     `json:"error,omitempty"`
	Created     []Resource `json:"created"`
	KeepData    bool       `json:"keep_data,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// Response is one installation as the API returns it.
type Response struct {
	ID          int64      `json:"id"`
	Owner       string     `json:"owner"`
	Repo        string     `json:"repo"`
	Release     string     `json:"release"`
	Commit      string     `json:"commit"`
	Project     string     `json:"project"`
	Environment string     `json:"environment"`
	Status      string     `json:"status"`
	Resources   []Resource `json:"resources"`
	// Runs is the most recent first; a listing carries only that one.
	Runs []RunResponse `json:"runs"`
	// Busy is a run changing it now: nothing else may start until it ends.
	Busy bool `json:"busy"`
	// UpdateAvailable is the newest release the catalog accepted, when the
	// installation is not on it.
	UpdateAvailable *string   `json:"update_available"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func toRunResponse(r *Run) RunResponse {
	created := r.Created
	if created == nil {
		created = []Resource{}
	}
	return RunResponse{
		ID: r.ID, Kind: r.Kind, FromRelease: r.FromRelease, ToRelease: r.ToRelease,
		Status: r.Status, Step: r.Step, Error: r.Error, Created: created, KeepData: r.KeepData,
		CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt,
	}
}

func toResponse(i Installed) Response {
	in := i.Install
	resources := in.Resources
	if resources == nil {
		resources = []Resource{}
	}
	out := Response{
		ID: in.ID, Owner: in.Owner, Repo: in.Repo, Release: in.Release, Commit: in.Commit,
		Project: in.Project, Environment: in.Environment, Status: in.Status, Resources: resources,
		Runs: []RunResponse{}, Busy: i.Busy(), CreatedAt: in.CreatedAt, UpdatedAt: in.UpdatedAt,
	}
	for _, r := range i.Runs {
		out.Runs = append(out.Runs, toRunResponse(r))
	}
	if i.Available != nil {
		out.UpdateAvailable = &i.Available.Tag
	}
	return out
}

// Names renames what a template declares, by key.
type Names struct {
	Databases map[string]string `json:"databases,omitempty"`
	Stores    map[string]string `json:"stores,omitempty"`
	Apps      map[string]string `json:"apps,omitempty"`
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /templates", auth(http.HandlerFunc(h.catalog)))
	r.Handle("GET /template-tags", auth(http.HandlerFunc(h.tags)))
	r.Handle("GET /templates/{owner}/{repo}", auth(http.HandlerFunc(h.template)))
	r.Handle("POST /templates/{owner}/{repo}/installs", auth(http.HandlerFunc(h.install)))
	r.Handle("GET /template-icons/{repository}/{file}", auth(http.HandlerFunc(h.icon)))
	r.Handle("GET /template-installs", auth(http.HandlerFunc(h.list)))
	r.Handle("GET /template-installs/{id}", auth(http.HandlerFunc(h.get)))
	r.Handle("GET /template-installs/{id}/update", auth(http.HandlerFunc(h.preview)))
	r.Handle("POST /template-installs/{id}/update", auth(http.HandlerFunc(h.update)))
	r.Handle("POST /template-installs/{id}/uninstall", auth(http.HandlerFunc(h.uninstall)))
}

// WriteError maps this module's errors onto status codes.
func WriteError(w http.ResponseWriter, err error) {
	var invalid *InvalidTemplateError
	var input *InputError
	var taken *TakenError
	switch {
	case errors.As(err, &invalid):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.As(err, &input), errors.Is(err, slug.ErrInvalid), errors.Is(err, slug.ErrReserved):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.As(err, &taken), errors.Is(err, ErrTooNew), errors.Is(err, ErrBusy),
		errors.Is(err, ErrNotInstalled), errors.Is(err, ErrUpToDate):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrTemplateNotFound), errors.Is(err, ErrReleaseNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrCatalogUnavailable):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		app.WriteError(w, err)
	}
}

func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	page, err := h.svc.Templates(r.Context(), user.FromContext(r.Context()), r.URL.Query())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeRaw(w, page)
}

func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.Tags(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeRaw(w, tags)
}

func (h *Handler) template(w http.ResponseWriter, r *http.Request) {
	found, err := h.svc.Template(r.Context(), user.FromContext(r.Context()), r.PathValue("owner"), r.PathValue("repo"))
	if err != nil {
		WriteError(w, err)
		return
	}
	writeRaw(w, found)
}

func writeRaw(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) icon(w http.ResponseWriter, r *http.Request) {
	icon, err := h.svc.Icon(r.Context(), user.FromContext(r.Context()), r.PathValue("repository"), r.PathValue("file"))
	if err != nil {
		WriteError(w, err)
		return
	}
	// The address carries the release's commit, so what it names never
	// changes.
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(icon)
}

func (h *Handler) install(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Release     string            `json:"release"`
		Project     string            `json:"project"`
		Environment string            `json:"environment"`
		Names       Names             `json:"names"`
		Inputs      map[string]string `json:"inputs"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	started, secrets, err := h.svc.Install(r.Context(), user.FromContext(r.Context()), Request{
		Owner: r.PathValue("owner"), Repo: r.PathValue("repo"), Release: req.Release,
		Project: req.Project, Environment: req.Environment,
		Databases: req.Names.Databases, Stores: req.Names.Stores, Apps: req.Names.Apps,
		Inputs: req.Inputs,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"install": toResponse(*started),
		"secrets": orEmpty(secrets),
	})
}

func orEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	installs, err := h.svc.Installs(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]Response, 0, len(installs))
	for _, i := range installs {
		out = append(out, toResponse(i))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func installID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, ErrNotFound.Error(), http.StatusNotFound)
		return 0, false
	}
	return id, true
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := installID(w, r)
	if !ok {
		return
	}
	found, err := h.svc.Get(r.Context(), user.FromContext(r.Context()), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(*found))
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	id, ok := installID(w, r)
	if !ok {
		return
	}
	preview, err := h.svc.PreviewUpdate(r.Context(), user.FromContext(r.Context()), id, r.URL.Query().Get("release"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := installID(w, r)
	if !ok {
		return
	}
	var req struct {
		Release string            `json:"release"`
		Inputs  map[string]string `json:"inputs"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	run, secrets, err := h.svc.Update(r.Context(), user.FromContext(r.Context()), id, UpdateRequest{Release: req.Release, Inputs: req.Inputs})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": toRunResponse(run), "secrets": orEmpty(secrets)})
}

func (h *Handler) uninstall(w http.ResponseWriter, r *http.Request) {
	id, ok := installID(w, r)
	if !ok {
		return
	}
	var req struct {
		KeepData *bool `json:"keep_data"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Keeping the data is what nobody saying otherwise gets.
	keep := req.KeepData == nil || *req.KeepData
	run, err := h.svc.Uninstall(r.Context(), user.FromContext(r.Context()), id, keep)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"run": toRunResponse(run)})
}
