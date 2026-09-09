package node

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/mesh"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Response is one machine as the API returns it.
//
// No credential is on it, at any role. A node's token is shown once, by
// the request that created it, and a listing that carried it would turn
// every read of this screen into a way out for every machine in the
// cluster.
type Response struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ControlPlane bool   `json:"control_plane"`
	Status       string `json:"status"`

	Address string `json:"address,omitempty"`
	Version string `json:"version,omitempty"`

	Cores            int   `json:"cores"`
	MemoryTotalBytes int64 `json:"memory_total_bytes"`
	DiskTotalBytes   int64 `json:"disk_total_bytes"`

	// The newest reading, absent until there is one.
	CPUPercent  *float64 `json:"cpu_percent,omitempty"`
	MemoryBytes *int64   `json:"memory_bytes,omitempty"`
	DiskBytes   *int64   `json:"disk_bytes,omitempty"`
	Containers  int      `json:"containers"`
	// InMesh is whether this machine is on the cluster's private
	// network. A machine can be `ready` and not on it — it is calling
	// in, and its containers cannot reach the others'.
	InMesh bool `json:"in_mesh"`

	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreatedResponse is the one answer that carries a credential, because
// it is the only moment it exists in a form anybody can read.
type CreatedResponse struct {
	Response
	// Token is what the machine's agent authenticates with. **Shown
	// once**: only its hash is stored, and nothing can hand it back.
	Token string `json:"token"`
}

func toResponse(n *Node) Response {
	return Response{
		Name: n.Slug, Description: n.Description, ControlPlane: n.ControlPlane,
		Status: n.Status(), Address: n.Address, Version: n.Version,
		Cores: n.Cores, MemoryTotalBytes: n.MemoryTotalBytes, DiskTotalBytes: n.DiskTotalBytes,
		CPUPercent: n.CPUPercent, MemoryBytes: n.MemoryBytes, DiskBytes: n.DiskBytes,
		Containers: n.Containers, InMesh: n.InMesh(),
		LastSeenAt: n.LastSeenAt, CreatedAt: n.CreatedAt,
	}
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes are two surfaces, and the difference is who is calling.
//
// The first three are an operator's, behind the same bearer key or
// session as everything else. The last is the **agent's**, behind a
// node's own credential — a different middleware and a different kind
// of caller, which is why it is registered with its own wrapper rather
// than with `auth`.
//
// The agent endpoint is `HandleInternal`: it is machinery, like the
// registry's webhook, and the OpenAPI document is the product's API
// rather than an inventory of routes.
func (h *Handler) Routes(r *httpx.Router, auth func(http.Handler) http.Handler) {
	r.Handle("GET /nodes", auth(http.HandlerFunc(h.list)))
	r.Handle("POST /nodes", auth(http.HandlerFunc(h.add)))
	r.Handle("GET /nodes/{name}", auth(http.HandlerFunc(h.get)))
	r.Handle("DELETE /nodes/{name}", auth(http.HandlerFunc(h.remove)))

	r.HandleInternal("POST /nodes/agent/reconcile", h.agent(http.HandlerFunc(h.reconcile)))
}

// nodeContextKey carries the authenticated machine into the handler.
type nodeContextKey struct{}

// agent authenticates a worker's own credential.
//
// It is deliberately not the user middleware with a special case: a
// node is not an account, holds no role, and must never be able to
// reach an endpoint that takes a caller. Keeping the two apart means
// the question "could a node do this" has one answer — only what is
// registered behind this wrapper.
func (h *Handler) agent(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, prefix) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		n, err := h.svc.Authenticate(r.Context(), strings.TrimPrefix(header, prefix))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nodeContextKey{}, n)))
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	nodes, err := h.svc.List(ctx, user.FromContext(ctx))
	if err != nil {
		WriteError(w, err)
		return
	}
	out := make([]Response, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toResponse(n))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := h.svc.Get(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(n))
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	ctx := r.Context()
	created, token, err := h.svc.Add(ctx, user.FromContext(ctx), req.Name, req.Description)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, CreatedResponse{Response: toResponse(created), Token: token})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	removed, err := h.svc.Remove(ctx, user.FromContext(ctx), r.PathValue("name"))
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(removed))
}

// AgentRequest is what a worker sends on every pass, and AgentResponse
// is what it is told.
//
// The shape is a **statement and an instruction**, not a query: the
// machine says what it is and what it is running, and is told what it
// should be running. Anything else — a node asking "may I", a control
// plane asking "are you there" — would be a second conversation to keep
// in step with this one.
type AgentRequest struct {
	Version string `json:"version"`
	// Address is where this machine is reached from outside, as it
	// worked it out. Empty is a legitimate answer and means it could
	// not — never a bridge address.
	Address string `json:"address"`

	Cores            int   `json:"cores"`
	MemoryTotalBytes int64 `json:"memory_total_bytes"`
	DiskTotalBytes   int64 `json:"disk_total_bytes"`

	CPUPercent  *float64 `json:"cpu_percent,omitempty"`
	MemoryBytes *int64   `json:"memory_bytes,omitempty"`
	DiskBytes   *int64   `json:"disk_bytes,omitempty"`

	Containers int `json:"containers"`
	// MeshNodeID is what this machine's own Engine says the swarm calls
	// it. Empty is a machine that is not on the cluster's network.
	MeshNodeID string `json:"mesh_node_id"`

	// Results are what the machine did with what it was told to run
	// since its last pass. Empty on a pass where nothing changed: a
	// container that was already running is not news.
	Results []Result `json:"results,omitempty"`
}

type AgentResponse struct {
	// Name is what this instance calls the machine. The agent logs it
	// on the first pass, so the box says which node it joined as.
	Name    string  `json:"name"`
	Desired Desired `json:"desired"`
	// Registry is this instance's own registry, as a host — the address
	// an image pushed here is pulled from.
	//
	// Sent rather than put in each placement's login, because the
	// credential for it is the machine's own: the control plane holds
	// only the hash of that, so what it can say is "images from here
	// are ours" and let the machine present what it already has.
	// Empty on an instance with no domain, which is one with no
	// registry to pull from.
	Registry string `json:"registry,omitempty"`

	// Mesh is what this machine needs to be on the cluster's private
	// network. Absent when there is none to be on — an instance whose
	// Docker cannot cluster, or one that could not bring the network
	// up this pass. The agent that gets none simply does not change
	// its own network, and asks again in ten seconds.
	Mesh *mesh.Info `json:"mesh,omitempty"`
	// IntervalSeconds is how long to wait before calling again. Served
	// rather than compiled into the agent, so a cluster's cadence is
	// the control plane's to change without every worker being upgraded.
	IntervalSeconds int `json:"interval_seconds"`
}

func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	n, _ := r.Context().Value(nodeContextKey{}).(*Node)
	if n == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req AgentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	desired, meshInfo, err := h.svc.Reconcile(r.Context(), n, Report{
		Version: req.Version, Address: req.Address,
		Cores: req.Cores, MemoryTotalBytes: req.MemoryTotalBytes, DiskTotalBytes: req.DiskTotalBytes,
		CPUPercent: req.CPUPercent, MemoryBytes: req.MemoryBytes, DiskBytes: req.DiskBytes,
		Containers: req.Containers, MeshNodeID: req.MeshNodeID,
	}, req.Results)
	if err != nil {
		WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, AgentResponse{
		Name:            n.Slug,
		Desired:         desired,
		Registry:        h.svc.RegistryHost(r.Context()),
		Mesh:            meshInfo,
		IntervalSeconds: int(Interval.Seconds()),
	})
}

func WriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrUnauthenticated):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	case errors.Is(err, user.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)

	case errors.Is(err, ErrNotFound), errors.Is(err, database.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrAlreadyExists):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrControlPlane):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrNoAddress), errors.Is(err, ErrHasApps):
		http.Error(w, err.Error(), http.StatusConflict)

	case errors.Is(err, ErrUnknownToken):
		http.Error(w, "unauthorized", http.StatusUnauthorized)

	case errors.Is(err, slug.ErrInvalid), errors.Is(err, slug.ErrReserved),
		errors.Is(err, ErrReservedSlug), errors.Is(err, httpx.ErrNotJSON):
		http.Error(w, err.Error(), http.StatusBadRequest)

	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
