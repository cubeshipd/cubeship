package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

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
	// Reference is the app's canonical identifier,
	// org/project/environment/name — also its registry repository path.
	Reference   string `json:"reference"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Domains are every name this app answers at, each with the port
	// behind it.
	Domains []DomainResponse `json:"domains"`
	// Image is the image this app is about, which depends on where it
	// comes from: for a registry app, where a push should go — empty
	// while the instance has no domain, because there is nowhere to push
	// yet — and for an external app, what it pulls.
	Image  string `json:"image,omitempty"`
	Status string `json:"status"`
	// Source is where this app's image comes from.
	Source string `json:"source"`
	// Repo, Ref and Dockerfile describe a building app's source. Absent
	// for one that does not build.
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Dockerfile  string `json:"dockerfile,omitempty"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

// toResponse needs the registry host, which is instance configuration
// rather than a property of the app, so it is passed in — resolved once
// per request instead of once per app in a listing.
func toResponse(a *Scoped, registryHost string) Response {
	r := Response{
		Reference: ReferenceOf(a).String(),
		Name:      a.Name, Description: a.Description, Domains: toDomains(a.Domains),
		Status: a.Status, Source: a.Source,
		Project: a.ProjectSlug, Environment: a.EnvironmentSlug,
	}
	switch Source(a.Source) {
	case SourceExternal:
		r.Image = a.SourceImage
	case SourceDockerfile, SourceRailpack:
		r.Repo, r.Ref, r.Dockerfile = a.SourceRepo, a.SourceRef, a.SourceDockerfile
	default:
		if registryHost != "" {
			r.Image = ReferenceOf(a).ImageFor(registryHost)
		}
	}
	return r
}

func toResponses(apps []*Scoped, registryHost string) []Response {
	out := make([]Response, 0, len(apps))
	for _, a := range apps {
		out = append(out, toResponse(a, registryHost))
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// An app is addressed by its four-part reference, because a name is only
// unique within its environment.
const appPath = "/apps/{project}/{env}/{name}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /apps", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /apps", auth(http.HandlerFunc(h.list)))
	r.Handle("GET "+appPath, auth(http.HandlerFunc(h.get)))
	r.Handle("PATCH "+appPath, auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE "+appPath, auth(http.HandlerFunc(h.delete)))
	r.Handle("POST "+appPath+"/deploy", auth(http.HandlerFunc(h.deploy)))
	r.Handle("GET "+appPath+"/deployments", auth(http.HandlerFunc(h.deployments)))
	r.Handle("GET "+appPath+"/deployments/{id}", auth(http.HandlerFunc(h.deployment)))
	r.Handle("POST "+appPath+"/domains", auth(http.HandlerFunc(h.addDomain)))
	r.Handle("PATCH "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.setDomainPort)))
	r.Handle("DELETE "+appPath+"/domains/{domainID}", auth(http.HandlerFunc(h.removeDomain)))
	r.Handle("GET "+appPath+"/env", auth(http.HandlerFunc(h.getEnv)))
	r.Handle("PUT "+appPath+"/env", auth(http.HandlerFunc(h.setEnv)))
	r.Handle("PATCH "+appPath+"/env", auth(http.HandlerFunc(h.mergeEnv)))
	r.Handle("GET "+appPath+"/logs", auth(http.HandlerFunc(h.logs)))
}

// refFrom builds the app reference from the request path.
func refFrom(r *http.Request) Reference {
	return Reference{
		Project:     r.PathValue("project"),
		Environment: r.PathValue("env"),
		Name:        r.PathValue("name"),
	}
}

// WriteError maps this module's domain errors onto status codes, falling
// through to project's (and so to org's) for what it re-raises.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrUnknownSource), errors.Is(err, ErrImageRequired),
		errors.Is(err, ErrImageNotAllowed), errors.Is(err, ErrImageCarriesTag),
		errors.Is(err, ErrRepoRequired), errors.Is(err, ErrRepoNotAllowed),
		errors.Is(err, ErrRepoNotSupported), errors.Is(err, ErrDockerfileNotAllowed):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrDomainRequired):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoBuilder):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoRegistry):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoContainer):
		http.Error(w, "app has no running container yet", http.StatusConflict)
	case errors.Is(err, ErrDeploymentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		project.WriteError(w, err)
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
		Source      string `json:"source"`
		// Image is where an external app pulls from, without a tag.
		Image string `json:"image"`
		// Repo, Ref and Dockerfile are where a building app builds from.
		Repo       string `json:"repo"`
		Ref        string `json:"ref"`
		Dockerfile string `json:"dockerfile"`
	}
	// The domain is not required: an app is created empty and made
	// deployable afterwards, in its own settings. Everything that says
	// *where the app is* still is.
	if err := httpx.DecodeJSON(r, &req); err != nil ||
		req.Name == "" || req.Project == "" {
		http.Error(w, "name and project are required", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()),
		req.Project, req.Environment, req.Name, req.Description, Source(req.Source),
		Origin{Image: req.Image, Repo: req.Repo, Ref: req.Ref, Dockerfile: req.Dockerfile})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(created, h.svc.RegistryHost(r.Context())))
}

// update is PATCH: a field left out of the body is left alone, so one
// section of the settings screen can be saved without sending the rest.
//
// The source and its origin fields are one field group, not four
// independent ones — naming a source without the settings it needs, or
// settings the new source would ignore, is what the service refuses.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
		Source      *string `json:"source"`
		Image       *string `json:"image"`
		Repo        *string `json:"repo"`
		Ref         *string `json:"ref"`
		Dockerfile  *string `json:"dockerfile"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	var source *Source
	if req.Source != nil {
		s := Source(*req.Source)
		source = &s
	}
	var origin *Origin
	if req.Image != nil || req.Repo != nil || req.Ref != nil || req.Dockerfile != nil {
		origin = &Origin{
			Image:      deref(req.Image),
			Repo:       deref(req.Repo),
			Ref:        deref(req.Ref),
			Dockerfile: deref(req.Dockerfile),
		}
	}
	if req.Description == nil && source == nil && origin == nil {
		http.Error(w, "nothing to change", http.StatusBadRequest)
		return
	}

	updated, err := h.svc.Update(r.Context(), user.FromContext(r.Context()), refFrom(r),
		req.Description, source, origin)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.RegistryHost(r.Context())))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	apps, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(apps, h.svc.RegistryHost(r.Context())))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), orgRoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(a, h.svc.RegistryHost(r.Context())))
}

// delete removes an app and the container serving it. Requires the
// member role — the same level that can deploy it.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if _, err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), refFrom(r)); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DeploymentResponse is one deploy attempt, as the API reports it.
type DeploymentResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	Image  string `json:"image"`
	Error  string `json:"error,omitempty"`
	// Logs is what a build printed. Absent for a source that only pulls.
	Logs      string    `json:"logs,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func toDeploymentResponse(d *Deployment) DeploymentResponse {
	return DeploymentResponse{
		ID: d.ID, Status: d.Status, Image: d.ImageRef, Error: d.Error,
		Logs: d.Logs, CreatedAt: d.CreatedAt,
	}
}

// deploy accepts a redeploy and answers 202 with the deployment that
// records it. The work runs detached, so hanging up here does not stop
// it — poll the deployment to find out how it went.
func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Tag string `json:"tag"`
	}
	// An empty or absent body is fine; Tag stays "" and defaults later.
	_ = httpx.DecodeJSON(r, &req)

	_, deployment, err := h.svc.Deploy(r.Context(), user.FromContext(r.Context()), refFrom(r), req.Tag)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toDeploymentResponse(deployment))
}

func (h *Handler) deployments(w http.ResponseWriter, r *http.Request) {
	history, err := h.svc.Deployments(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]DeploymentResponse, 0, len(history))
	for _, d := range history {
		out = append(out, toDeploymentResponse(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// deployment reads one deploy. `?wait=true` holds the response open
// until it finishes, which is what a CLI wants — but the deploy runs
// regardless of whether anyone waits.
func (h *Handler) deployment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid deployment id", http.StatusBadRequest)
		return
	}

	ctx, caller, ref := r.Context(), user.FromContext(r.Context()), refFrom(r)

	var d *Deployment
	if r.URL.Query().Get("wait") == "true" {
		d, err = h.svc.WaitForDeployment(ctx, caller, ref, id)
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			// The wait ran out, not the deploy. Hand back where it had
			// got to and let the caller ask again.
			httpx.WriteJSON(w, http.StatusOK, toDeploymentResponse(d))
			return
		}
	} else {
		d, err = h.svc.Deployment(ctx, caller, ref, id)
	}
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toDeploymentResponse(d))
}

// EnvResponse is what reading an app's variables returns: the ones set
// on the app itself, and the full set its container runs with, each
// value labelled with the level it came from.
type EnvResponse struct {
	Vars      envvar.Map        `json:"vars"`
	Effective []envvar.Resolved `json:"effective"`
}

func (h *Handler) getEnv(w http.ResponseWriter, r *http.Request) {
	own, effective, err := h.svc.Env(r.Context(), user.FromContext(r.Context()), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, EnvResponse{Vars: own, Effective: effective})
}

// MergeEnvRequest adds or overwrites the variables in set and removes
// those named in unset. Anything not mentioned is left alone — which is
// the difference between this and PUT.
type MergeEnvRequest struct {
	Set   envvar.Map `json:"set"`
	Unset []string   `json:"unset"`
}

func (h *Handler) mergeEnv(w http.ResponseWriter, r *http.Request) {
	var req MergeEnvRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if _, err := h.svc.MergeEnv(r.Context(), user.FromContext(r.Context()),
		refFrom(r), req.Set, req.Unset); err != nil {
		WriteError(w, err)
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
	if _, err := h.svc.SetEnv(r.Context(), user.FromContext(r.Context()), refFrom(r), req.Vars); err != nil {
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

	rc, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()), refFrom(r), tail)
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
		log.Printf("logs for app %s: %v", refFrom(r), err)
	}
}

// DomainResponse is one name an app answers at.
type DomainResponse struct {
	ID   int64  `json:"id"`
	Host string `json:"host"`
	// Port is what this name reaches, or 0 for "read it from the
	// image". Zero is the normal answer.
	Port int `json:"port"`
}

func toDomains(domains []Domain) []DomainResponse {
	out := make([]DomainResponse, 0, len(domains))
	for _, d := range domains {
		out = append(out, DomainResponse{ID: d.ID, Host: d.Host, Port: d.Port})
	}
	return out
}

func (h *Handler) addDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
		// Omitted or 0 means DefaultPort.
		Port int `json:"port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	updated, err := h.svc.AddDomain(ctx, user.FromContext(ctx), refFrom(r), req.Host, req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(updated, h.svc.RegistryHost(ctx)))
}

func (h *Handler) setDomainPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Port int `json:"port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, ok := domainIDFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	updated, err := h.svc.SetDomainPort(ctx, user.FromContext(ctx), refFrom(r), id, req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.RegistryHost(ctx)))
}

func (h *Handler) removeDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := domainIDFrom(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	updated, err := h.svc.RemoveDomain(ctx, user.FromContext(ctx), refFrom(r), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.RegistryHost(ctx)))
}

// domainIDFrom reads the domain's id, answering 404 for anything that is
// not one: a path segment that is not a number names nothing, and that
// is the same answer as naming something that does not exist.
func domainIDFrom(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("domainID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}
