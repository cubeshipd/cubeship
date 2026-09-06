package datastore

import (
	"errors"
	"log"
	"net/http"
	"slices"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

// Response is one datastore as both the API and the MCP tools report
// it.
//
// The password is not in it, and that is the one deliberate omission:
// everything else here is worth having on a screen that lists
// databases, and a credential is worth asking for on purpose. See
// CredentialsResponse.
type Response struct {
	// Name identifies it on the whole instance. There is nothing above
	// a datastore, so there is no longer a reference to build.
	Name        string `json:"name"`
	Description string `json:"description"`

	Engine  string `json:"engine"`
	Version string `json:"version"`
	Status  string `json:"status"`
	// Error is why provisioning failed, when it did.
	Error string `json:"error,omitempty"`

	Username string `json:"username"`
	Database string `json:"database,omitempty"`

	// HasContainer says whether a container currently backs this. It is
	// what decides whether there is a log to read or anything to stop —
	// which the status alone cannot answer, since a datastore whose
	// provisioning failed may have neither.
	HasContainer bool `json:"has_container"`

	// Host and Port are where an app on this instance reaches it: the
	// container's own name on the shared network. Every attached app
	// already has these as variables — they are here for the person
	// reading the screen.
	Host string `json:"host"`
	Port int    `json:"port"`

	// ExposedPort is the host port it also answers on from outside,
	// absent when it does not. ExternalHost is the instance's own
	// domain, absent while there is none to name.
	ExposedPort  int    `json:"exposed_port,omitempty"`
	ExternalHost string `json:"external_host,omitempty"`

	// Attachments are the apps that receive this datastore's connection
	// variables, in any project and any environment.
	Attachments []AttachmentResponse `json:"attachments"`

	CreatedAt time.Time `json:"created_at"`
}

// AttachmentResponse is one app wired to a datastore, and what the
// variables it receives are called.
//
// Names without values: the password is one of the values, and a list
// of what an app is given should not be a way to read it.
type AttachmentResponse struct {
	// App is the app's full reference, project/environment/name — a
	// bare name would identify nothing, since a datastore is not inside
	// an environment and one may serve apps in several.
	App    string `json:"app"`
	Prefix string `json:"prefix,omitempty"`
	// Variables are the names this app's container receives, sorted.
	Variables []string `json:"variables"`
}

// CreatedResponse is a datastore plus the password it was created with,
// shown once — the same shape a new API key is reported in.
//
// Not because it cannot be read again — an admin can, from
// GET .../credentials — but because the caller that just generated one
// by leaving the field empty is the caller who needs to see it.
type CreatedResponse struct {
	Response
	Password string `json:"password"`
}

// CredentialsResponse is the login and where to use it.
type CredentialsResponse struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database,omitempty"`

	// InternalURI is what an app on this instance connects with. It is
	// what an attached app already receives as DATABASE_URL; this is
	// for anything wired by hand.
	InternalURI  string `json:"internal_uri"`
	InternalHost string `json:"internal_host"`
	InternalPort int    `json:"internal_port"`

	// ExternalURI is the same from off this host, present only while
	// the datastore is exposed and the instance has a domain.
	ExternalURI  string `json:"external_uri,omitempty"`
	ExternalHost string `json:"external_host,omitempty"`
	ExternalPort int    `json:"external_port,omitempty"`
}

// EngineResponse is one engine this release can run. It exists so a
// client offers the versions the daemon will actually accept rather
// than a list of its own that drifts out of step with it.
type EngineResponse struct {
	Engine         string   `json:"engine"`
	Versions       []string `json:"versions"`
	DefaultVersion string   `json:"default_version"`
	// Port is what the engine listens on, which is what an app connects
	// to inside the network.
	Port int `json:"port"`
	// HasDatabase says whether naming a database inside the server
	// means anything for this engine.
	HasDatabase bool `json:"has_database"`
	// HasUser says whether the login is somebody's to choose. False for
	// Redis, whose password belongs to a user that already exists.
	HasUser bool `json:"has_user"`
	// DefaultUsername is the login an empty username becomes — and the
	// only one there is when HasUser is false.
	DefaultUsername string `json:"default_username"`
}

// Instance is what a response depends on that the datastore itself does
// not carry. Read once per request rather than once per row.
type Instance struct {
	// Domain is the instance's own, which is where an exposed datastore
	// is reached. Empty before there is one.
	Domain string
}

func toResponse(d *Datastore, in Instance) Response {
	host := ContainerName(d.Slug)
	r := Response{
		Name: d.Slug, Description: d.Description,
		Engine: string(d.Engine), Version: d.Version,
		Status: d.Status, Error: d.Error,
		Username: d.Username, Database: d.Database,
		HasContainer: d.ContainerID != "",
		Host:         host, Port: d.Engine.Port(),
		ExposedPort: d.ExposedPort,
		Attachments: make([]AttachmentResponse, 0, len(d.Attachments)),
		CreatedAt:   d.CreatedAt,
	}
	if d.ExposedPort != 0 {
		r.ExternalHost = in.Domain
	}
	for _, a := range d.Attachments {
		names := make([]string, 0, 6)
		for name := range d.Vars(a.Prefix, host, d.Engine.Port()) {
			names = append(names, name)
		}
		slices.Sort(names)
		r.Attachments = append(r.Attachments, AttachmentResponse{
			App: a.AppReference, Prefix: a.Prefix, Variables: names,
		})
	}
	return r
}

func toResponses(all []*Datastore, in Instance) []Response {
	out := make([]Response, 0, len(all))
	for _, d := range all {
		out = append(out, toResponse(d, in))
	}
	return out
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// A datastore is addressed by its name alone. It belongs to the
// instance, so there is no project or environment in front of it — and
// the name is unique across the instance for the same reason: it is the
// container.
const datastorePath = "/datastores/{name}"

// attachmentPath addresses one wiring. The app takes all three of its
// own segments, because an attachment may name an app in any project.
const attachmentPath = datastorePath + "/attachments/{project}/{env}/{app}"

func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("POST /datastores", auth(http.HandlerFunc(h.create)))
	r.Handle("GET /datastores", auth(http.HandlerFunc(h.list)))
	// A literal beats a wildcard in Go's mux, so this wins over
	// {name} — which is also why a datastore may not be called
	// "engines". See reservedSlugs.
	r.Handle("GET /datastores/engines", auth(http.HandlerFunc(h.engines)))
	r.Handle("GET "+datastorePath, auth(http.HandlerFunc(h.get)))
	r.Handle("PATCH "+datastorePath, auth(http.HandlerFunc(h.update)))
	r.Handle("DELETE "+datastorePath, auth(http.HandlerFunc(h.delete)))
	r.Handle("GET "+datastorePath+"/credentials", auth(http.HandlerFunc(h.credentials)))
	r.Handle("GET "+datastorePath+"/metrics", auth(http.HandlerFunc(h.metrics)))
	r.Handle("GET "+datastorePath+"/logs", auth(http.HandlerFunc(h.logs)))
	r.Handle("POST "+datastorePath+"/stop", auth(http.HandlerFunc(h.stop)))
	r.Handle("POST "+datastorePath+"/start", auth(http.HandlerFunc(h.start)))
	r.Handle("POST "+datastorePath+"/expose", auth(http.HandlerFunc(h.expose)))
	r.Handle("DELETE "+datastorePath+"/expose", auth(http.HandlerFunc(h.unexpose)))
	r.Handle("POST "+datastorePath+"/attachments", auth(http.HandlerFunc(h.attach)))
	r.Handle("DELETE "+attachmentPath, auth(http.HandlerFunc(h.detach)))
}

func nameFrom(r *http.Request) string { return r.PathValue("name") }

// appRefFrom rebuilds the app reference the detach path carries.
func appRefFrom(r *http.Request) string {
	return r.PathValue("project") + "/" + r.PathValue("env") + "/" + r.PathValue("app")
}

// WriteError maps this module's domain errors onto status codes, falling
// through to app's — and so to project's and user's — for what it
// re-raises.
//
// app's, not project's. The chain has to mirror the dependency chain,
// and this module resolves apps: attaching names one, so app.ErrNotFound
// reaches here. Skipping a link meant "no such app" arriving as a 500
// with the right sentence in it, which is the worst of both — a caller
// retries a 500 and reads a 404.
func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNotAttached):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrNotRunning):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrAlreadyExists), errors.Is(err, ErrAlreadyAttached),
		errors.Is(err, ErrPrefixTaken), errors.Is(err, ErrPortTaken),
		errors.Is(err, ErrNoPortsLeft):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrUnknownEngine), errors.Is(err, ErrUnknownVersion),
		errors.Is(err, ErrBadUsername), errors.Is(err, ErrFixedUsername),
		errors.Is(err, ErrBadPrefix),
		errors.Is(err, ErrBadPort), errors.Is(err, ErrReservedSlug):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		app.WriteError(w, err)
	}
}

func (h *Handler) instance(r *http.Request) Instance {
	return Instance{Domain: h.svc.externalHost(r.Context())}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Engine      string `json:"engine"`
		Version     string `json:"version"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Database    string `json:"database"`
		// Expose asks for a host port at creation. Null is "internal
		// only", 0 is "pick one".
		Expose *int `json:"expose"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" || req.Engine == "" {
		http.Error(w, "name and engine are required", http.StatusBadRequest)
		return
	}
	created, err := h.svc.Create(r.Context(), user.FromContext(r.Context()), Spec{
		Slug: req.Name, Description: req.Description,
		Engine: Engine(req.Engine), Version: req.Version,
		Username: req.Username, Password: req.Password, Database: req.Database,
		Expose: req.Expose,
	})
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, CreatedResponse{
		Response: toResponse(created, h.instance(r)),
		// Whatever it ended up being: what was sent, or what was
		// generated because nothing was.
		Password: created.Password,
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	all, err := h.svc.List(r.Context(), user.FromContext(r.Context()))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponses(all, h.instance(r)))
}

func (h *Handler) engines(w http.ResponseWriter, r *http.Request) {
	if err := user.Require(user.FromContext(r.Context()), user.RoleMember); err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, engineResponses())
}

func engineResponses() []EngineResponse {
	out := make([]EngineResponse, 0, len(Engines()))
	for _, e := range Engines() {
		out = append(out, EngineResponse{
			Engine: string(e), Versions: e.Versions(),
			DefaultVersion: e.DefaultVersion(), Port: e.Port(),
			HasDatabase: e.HasDatabase(),
			HasUser:     e.HasUser(), DefaultUsername: e.DefaultUsername(),
		})
	}
	return out
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), nameFrom(r), user.RoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

// update is PATCH: a field left out is left alone. There is one field,
// and Service.Update says why the others cannot be among them.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	d, err := h.svc.Update(r.Context(), user.FromContext(r.Context()), nameFrom(r), req.Description)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Delete(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

// metrics is what this database's container has been using. The series
// is metrics' to render; what this adds is who may look at it.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), nameFrom(r), user.RoleMember)
	if err != nil {
		WriteError(w, err)
		return
	}
	metrics.WriteSeries(w, r, h.svc.Metrics(), metrics.KindDatastore, d.ID, d.ContainerID != "")
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	rc, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()),
		nameFrom(r), r.URL.Query().Get("tail"))
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
		log.Printf("logs for datastore %s: %v", nameFrom(r), err)
	}
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Stop(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Start(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

func (h *Handler) credentials(w http.ResponseWriter, r *http.Request) {
	creds, err := h.svc.Credentials(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CredentialsResponse{
		Username: creds.Username, Password: creds.Password, Database: creds.Database,
		InternalURI: creds.Internal, InternalHost: creds.InternalHost, InternalPort: creds.InternalPort,
		ExternalURI: creds.External, ExternalHost: creds.ExternalHost, ExternalPort: creds.ExternalPort,
	})
}

func (h *Handler) expose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// Port 0, or an absent body, means "pick one".
		Port int `json:"port"`
	}
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
	}
	d, err := h.svc.Expose(r.Context(), user.FromContext(r.Context()), nameFrom(r), req.Port)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

func (h *Handler) unexpose(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Unexpose(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		App    string `json:"app"`
		Prefix string `json:"prefix"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || req.App == "" {
		http.Error(w, "app is required", http.StatusBadRequest)
		return
	}
	d, err := h.svc.Attach(r.Context(), user.FromContext(r.Context()), nameFrom(r), req.App, req.Prefix)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(d, h.instance(r)))
}

func (h *Handler) detach(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Detach(r.Context(), user.FromContext(r.Context()), nameFrom(r), appRefFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}
