package datastore

import (
	"log"
	"net/http"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

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
			VarStem:    e.VarStem(),
			Extensions: engineExtensions(e),
		})
	}
	return out
}

// engineExtensions is what e offers, each with the versions it is
// offered at. Built from the same matrix the service validates against,
// so a form cannot offer a combination creation would refuse.
func engineExtensions(e Engine) []EngineExtensionResponse {
	byName := map[Extension][]string{}
	for _, v := range e.Versions() {
		for _, x := range SupportedExtensions(e, v) {
			byName[x] = append(byName[x], v)
		}
	}
	out := make([]EngineExtensionResponse, 0, len(byName))
	for _, x := range AllExtensions() {
		versions, ok := byName[x]
		if !ok {
			continue
		}
		requires := make([]string, 0, len(x.Requires()))
		for _, dep := range x.Requires() {
			requires = append(requires, string(dep))
		}
		out = append(out, EngineExtensionResponse{
			Name: string(x), SQLName: x.SQLName(), Summary: x.Summary(),
			Requires: requires, Versions: versions, Builtin: x.Builtin(),
		})
	}
	return out
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), nameFrom(r), user.LevelView)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d, h.instance(r)))
}

// update is PATCH: a field left out is left alone. There are two, and
// Service.Update says why the others cannot be among them.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Description *string `json:"description"`
		// Extensions has no business here and is accepted only so that
		// sending it is refused in words rather than dropped. Adding one
		// replaces the container, which is not what a PATCH of a
		// description does.
		Extensions *[]string `json:"extensions"`
		// Limits is how much of the machine this database may take.
		// Sent as an object so that clearing a ceiling is a value
		// rather than a gap: leaving the field out keeps what is
		// there, and sending zeros removes it.
		Limits *Limits `json:"limits"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	d, err := h.svc.Update(r.Context(), user.FromContext(r.Context()), nameFrom(r),
		req.Description, req.Limits, req.Extensions)
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
	d, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), nameFrom(r), user.LevelView)
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
	h.credentialsResponse(w, creds)
}

func (h *Handler) rotateCredentials(w http.ResponseWriter, r *http.Request) {
	creds, err := h.svc.RotatePassword(r.Context(), user.FromContext(r.Context()), nameFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	h.credentialsResponse(w, creds)
}

func (h *Handler) credentialsResponse(w http.ResponseWriter, creds Credentials) {
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

// addExtensions installs extensions on a database that already exists.
//
// POST rather than PATCH, and its own address rather than a field on the
// datastore, because it is not an edit: it replaces the container, and
// it only ever adds. What comes back is the datastore in
// "provisioning" — the replacement happens detached, like every other
// provision here.
func (h *Handler) addExtensions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Extensions []string `json:"extensions"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil || len(req.Extensions) == 0 {
		http.Error(w, "extensions is required, and is a non-empty list of names", http.StatusBadRequest)
		return
	}
	d, err := h.svc.AddExtensions(r.Context(), user.FromContext(r.Context()), nameFrom(r), req.Extensions)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toResponse(d, h.instance(r)))
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
