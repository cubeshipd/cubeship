package app

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
)

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

// metrics is what this app's container has been using. The series
// itself is metrics' to render — see metrics.WriteSeries — and what
// this adds is the one thing that package must not decide: who is
// allowed to look.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	a, err := h.svc.Resolve(r.Context(), user.FromContext(r.Context()), refFrom(r), user.LevelView)
	if err != nil {
		WriteError(w, err)
		return
	}
	metrics.WriteSeries(w, r, h.svc.Metrics(), metrics.KindApp, a.ID, a.HasContainer())
}

func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = DefaultLogTail
	}

	// A log belongs to one container and so to one machine. `server`
	// names which; leaving it off gets the machine the app's traffic
	// arrives at, which is the one somebody looking at a name means.
	server := r.URL.Query().Get("server")

	rc, err := h.svc.Logs(r.Context(), user.FromContext(r.Context()), refFrom(r), server, tail)
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
	httpx.WriteJSON(w, http.StatusCreated, toResponse(updated, h.svc.InstanceConfig(ctx)))
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
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.InstanceConfig(ctx)))
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
	httpx.WriteJSON(w, http.StatusOK, toResponse(updated, h.svc.InstanceConfig(ctx)))
}

// domainIDFrom reads the domain's id, answering 404 for anything that is
// not one: a path segment that is not a number names nothing, and that
// is the same answer as naming something that does not exist.
// VolumeResponse is one of an app's volumes.
type VolumeResponse struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	Node      string    `json:"node"`
	CreatedAt time.Time `json:"created_at"`
}

func toVolumeResponse(v Volume) VolumeResponse {
	return VolumeResponse{ID: v.ID, Path: v.Path, Node: v.NodeSlug, CreatedAt: v.CreatedAt}
}

// OrphanResponse is a volume's data kept after the volume was removed.
type OrphanResponse struct {
	ID        int64      `json:"id"`
	App       string     `json:"app,omitempty"`
	Path      string     `json:"path,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
}

func (h *Handler) listVolumes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	volumes, err := h.svc.Volumes(ctx, user.FromContext(ctx), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]VolumeResponse, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, toVolumeResponse(v))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) addVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	v, err := h.svc.AddVolume(ctx, user.FromContext(ctx), refFrom(r), req.Path)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toVolumeResponse(*v))
}

func (h *Handler) removeVolume(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("volumeID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	deleteData := r.URL.Query().Get("delete_data") == "true"
	if err := h.svc.RemoveVolume(ctx, user.FromContext(ctx), refFrom(r), id, deleteData); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) volumeOrphans(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	found, err := h.svc.VolumeOrphans(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]OrphanResponse, 0, len(found))
	for _, o := range found {
		resp := OrphanResponse{ID: o.ID, App: o.App, Path: o.Path}
		if !o.CreatedAt.IsZero() {
			at := o.CreatedAt
			resp.CreatedAt = &at
		}
		out = append(out, resp)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) deleteVolumeOrphan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.DeleteVolumeOrphan(ctx, user.FromContext(ctx), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TCPPortResponse is one of an app's published ports.
type TCPPortResponse struct {
	ID            int64     `json:"id"`
	ContainerPort int       `json:"container_port"`
	HostPort      int       `json:"host_port"`
	CreatedAt     time.Time `json:"created_at"`
}

func toTCPPortResponse(p TCPPort) TCPPortResponse {
	return TCPPortResponse{ID: p.ID, ContainerPort: p.ContainerPort, HostPort: p.HostPort, CreatedAt: p.CreatedAt}
}

func (h *Handler) listTCPPorts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ports, err := h.svc.TCPPorts(ctx, user.FromContext(ctx), refFrom(r))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]TCPPortResponse, 0, len(ports))
	for _, p := range ports {
		out = append(out, toTCPPortResponse(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) addTCPPort(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContainerPort int `json:"container_port"`
		HostPort      int `json:"host_port"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	p, err := h.svc.AddTCPPort(ctx, user.FromContext(ctx), refFrom(r), req.ContainerPort, req.HostPort)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toTCPPortResponse(*p))
}

func (h *Handler) removeTCPPort(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("portID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ctx := r.Context()
	if err := h.svc.RemoveTCPPort(ctx, user.FromContext(ctx), refFrom(r), id); err != nil {
		WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func domainIDFrom(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("domainID"), 10, 64)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return 0, false
	}
	return id, true
}
