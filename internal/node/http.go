package node

import (
	"context"
	"errors"
	"io"
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
	r.HandleInternal("POST /nodes/agent/results/{id}", h.agent(http.HandlerFunc(h.result)))
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

	// Wait asks the control plane to hold this request open when it has
	// nothing to say, rather than answering an empty poll at once.
	//
	// The machine's to decide, not this instance's, and that is what
	// makes it safe to add: an agent from before this existed does not
	// send it, is answered immediately, and goes on polling on its own
	// interval exactly as it did. What it buys the ones that do send it
	// is hearing about a deploy, or being asked for a log, in the
	// moment it happens.
	Wait bool `json:"wait,omitempty"`

	// Results are what the machine did with what it was told to run
	// since its last pass. Empty on a pass where nothing changed: a
	// container that was already running is not news.
	Results []Result `json:"results,omitempty"`

	// Readings are what the containers it runs are using. Empty on most
	// passes: a machine polls far more often than a chart wants a
	// point, so it takes one on the interval the control plane samples
	// its own containers on.
	Readings []Reading `json:"readings,omitempty"`
}

type AgentResponse struct {
	// Name is what this instance calls the machine. The agent logs it
	// on the first pass, so the box says which node it joined as.
	Name    string  `json:"name"`
	Desired Desired `json:"desired"`
	// Commands are what this instance is asking the machine to do
	// right now — read a log, and in time more. Empty on almost every
	// poll: this is the channel that lets the control plane ask
	// anything at all, and most of the time it has nothing to ask.
	Commands []Command `json:"commands,omitempty"`
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

	// Edge is what a machine needs to serve an app's own names: each
	// one runs its own Traefik, because a name pointing at the control
	// plane reaches nothing when the app is somewhere else.
	//
	// Sent on every poll and acted on only when there is something to
	// route — the machine can see that in what it was told to run, and
	// a worker running nothing with a name has no reason to hold two
	// ports open.
	Edge *Edge `json:"edge,omitempty"`

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
	ctx := r.Context()
	n, _ := ctx.Value(nodeContextKey{}).(*Node)
	if n == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req AgentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	desired, meshInfo, err := h.svc.Reconcile(ctx, n, Report{
		Version: req.Version, Address: req.Address,
		Cores: req.Cores, MemoryTotalBytes: req.MemoryTotalBytes, DiskTotalBytes: req.DiskTotalBytes,
		CPUPercent: req.CPUPercent, MemoryBytes: req.MemoryBytes, DiskBytes: req.DiskBytes,
		Containers: req.Containers, MeshNodeID: req.MeshNodeID,
	}, req.Results, req.Readings)
	if err != nil {
		WriteError(w, err)
		return
	}
	answer := AgentResponse{
		Name:            n.Slug,
		Edge:            h.svc.EdgeConfig(ctx),
		Desired:         desired,
		Registry:        h.svc.RegistryHost(ctx),
		Mesh:            meshInfo,
		IntervalSeconds: int(Interval.Seconds()),
		Commands:        h.svc.hub.Take(n.ID),
	}

	// Nothing being asked of it, so the request waits rather than the
	// machine.
	//
	// This is what makes a channel that only goes one way feel like
	// both: a poll held open is a machine that hears about a deploy, or
	// is asked for a log, in the moment it happens rather than on its
	// next interval. It parks on **commands** rather than on whether
	// there is desired state, because there always is — a machine with
	// an app on it would otherwise never park, and never hear anything
	// promptly again.
	//
	// Waking is not the same as having something: the poll comes back
	// either way, and an empty answer is how the next one starts.
	if req.Wait && len(answer.Commands) == 0 {
		if h.svc.hub.Park(ctx, n.ID) {
			answer.Commands = h.svc.hub.Take(n.ID)
			// Asked again, because what it should be running may be
			// exactly what woke it.
			answer.Desired = h.svc.Desired(ctx, n)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, answer)
}

// result is a machine answering a command.
//
// The body is the answer, as bytes: a log is bytes, and a JSON string
// would replace whatever in it is not valid UTF-8. A machine that could
// not do what it was asked says so in `error` and sends nothing.
func (h *Handler) result(w http.ResponseWriter, r *http.Request) {
	n, _ := r.Context().Value(nodeContextKey{}).(*Node)
	if n == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var failed error
	if said := r.URL.Query().Get("error"); said != "" {
		failed = errors.New(said)
	}
	output, err := io.ReadAll(io.LimitReader(r.Body, MaxAnswerBytes))
	if err != nil {
		http.Error(w, "could not read the answer", http.StatusBadRequest)
		return
	}
	h.svc.hub.Answer(n.ID, r.PathValue("id"), output, failed)
	w.WriteHeader(http.StatusNoContent)
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
