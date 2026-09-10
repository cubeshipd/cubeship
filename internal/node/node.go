// Package node is the machines this instance is made of.
//
// One Cubeship is a **control plane** and any number of **workers**.
// The control plane is the instance somebody installed: it has the
// database, the dashboard, the registry and the builder, and it is the
// only thing that decides anything. A worker is a second box running
// the same daemon in a mode where it decides nothing — it holds no
// database, serves no dashboard, publishes no port, and does what it is
// told.
//
// **The worker dials the control plane, never the other way round.**
// That is the load-bearing decision here and it is worth stating: a
// control plane that called into its workers would mean every one of
// them publishing an authenticated API to the internet, with a
// certificate and a firewall hole each, and a box behind NAT could not
// join at all. Dialling out costs a reconcile loop and buys a worker
// with no inbound port whatsoever — which is the same argument
// internal/firewall makes about published ports, from the other side.
//
// So this module is two surfaces that share a table. One is the
// operator's — list the cluster, add a machine to it, remove one — and
// the other is the agent's, which is one endpoint the workers call and
// nobody else does.
package node

import (
	"context"
	"errors"
	"strconv"
	"time"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/user"
)

// ControlPlaneSlug names the row that is this machine. It is seeded by
// the migration and is not something anybody creates.
const ControlPlaneSlug = "control-plane"

// RegistryUsername is what a machine logs in to this instance's own
// registry as, to pull an image it was told to run.
//
// A fixed name rather than the machine's own, because it is not an
// account: what identifies the caller is its credential, and Basic auth
// merely has a field for a name. See internal/registry, which grants a
// machine pull and nothing else.
const RegistryUsername = "cubeship-node"

// LabelApp, LabelDeploy and LabelOrdinal are what a placed container
// carries, and they are how a machine tells its own work apart from
// everything else on the box: what app it belongs to, which deploy of
// it, and which copy of that app on this machine it is. A container
// with none of them is not this instance's to reason about.
//
// The ordinal is what makes a rolling deploy possible on a machine
// running several copies: an old container may go once **its own**
// replacement is up, and without it the agent can only ask whether some
// container of that app is running — which either retires a copy whose
// replacement never came, or leaves every old copy behind.
//
// A container from before this label carries none, and is read as the
// first copy — the only one an agent from then could have been running.
const (
	LabelApp     = "cubeship.app"
	LabelDeploy  = "cubeship.deploy"
	LabelOrdinal = "cubeship.ordinal"
)

// FirstOrdinal is the copy every app has. An ordinal that is missing or
// zero is this one, on both ends of the wire: it is what an app that has
// never been scaled out is running, and what a container created before
// there could be more than one is.
const FirstOrdinal = 1

// OrdinalOf reads a copy number that may not be there.
func OrdinalOf(n int) int {
	if n < FirstOrdinal {
		return FirstOrdinal
	}
	return n
}

// OrdinalFromLabels reads which copy a running container is, from what
// it was created with.
func OrdinalFromLabels(labels map[string]string) int {
	n, err := strconv.Atoi(labels[LabelOrdinal])
	if err != nil {
		return FirstOrdinal
	}
	return OrdinalOf(n)
}

// RoleToManage is what adding or removing a machine takes. An admin's:
// a node runs other people's code on hardware somebody pays for, and
// adding one hands out a credential.
const RoleToManage = user.RoleAdmin

// RoleToRead is what seeing the cluster takes. A member's, like the
// machine's own numbers — which box an app is on is part of knowing why
// it is slow, and none of it is a secret. The token is never in a
// listing whatever the role.
const RoleToRead = user.RoleMember

// Interval is how often a worker calls home.
//
// Ten seconds, not the thirty the metric collector uses: this is the
// clock the whole cluster reacts on, and later it is how long a deploy
// waits before the node it was placed on hears about it. It is one
// small request per node, so a cluster of ten costs a request a second.
const Interval = 10 * time.Second

// MissedBeats is how many passes a node may skip before it is reported
// unreachable.
//
// Three, so a single slow pass — a reconcile that took longer than the
// interval, a daemon restarting after an upgrade — does not flip a
// healthy machine to a fault and back again on the next screen refresh.
const MissedBeats = 3

// Statuses a node reports. They are **derived from `last_seen_at` on
// every read**, never stored: a status in a column is a status
// something has to run on a timer to keep true, and the moment that
// timer misses a pass the table says a machine is up that is not.
const (
	// StatusReady is a node that called within the last few passes, and
	// what the control plane itself always is.
	StatusReady = "ready"
	// StatusPending is a node that has been created here and has never
	// connected. It is waiting for somebody to run the install command
	// on the machine.
	StatusPending = "pending"
	// StatusUnreachable is a node that has connected before and has
	// stopped. It says nothing about why: the box may be off, the
	// daemon may be down, or the network between here and there may be.
	StatusUnreachable = "unreachable"
)

// Node is one machine in this instance's cluster.
type Node struct {
	ID           int64
	Slug         string
	Description  string
	ControlPlane bool

	// Address is where this machine is reached from outside, as the
	// machine itself reported it. Empty when it has not said — a node
	// that cannot work out its own public address reports none rather
	// than a bridge address, the same rule settings.Routable enforces
	// for this instance's own.
	Address string
	// Version is the daemon the agent is running, which is what says
	// whether a node is behind after an upgrade.
	Version string

	// The machine's facts, as of the last pass.
	Cores            int
	MemoryTotalBytes int64
	DiskTotalBytes   int64

	// The newest reading. Nil until one has been taken, because zero is
	// a reading and this is the absence of one.
	CPUPercent  *float64
	MemoryBytes *int64
	DiskBytes   *int64
	// MeshNodeID is what the swarm calls this machine, as the agent
	// last reported it. Empty means it is not on the cluster's private
	// network — it has not joined, or it could not.
	MeshNodeID string

	LastSeenAt *time.Time
	CreatedAt  time.Time
}

// Status is what this node is, worked out from when it last called.
func (n *Node) Status() string {
	// The control plane is the process answering this question. It
	// cannot be unreachable from itself, and saying so on a timer would
	// be this daemon reporting on whether it is running.
	if n.ControlPlane {
		return StatusReady
	}
	if n.LastSeenAt == nil {
		return StatusPending
	}
	if time.Since(*n.LastSeenAt) > Interval*MissedBeats {
		return StatusUnreachable
	}
	return StatusReady
}

// Report is what an agent says about its machine on every pass.
//
// It is a statement, not a request: everything in it is overwritten on
// the node's row, because the machine is the only thing that knows any
// of it and the newest answer is the only one worth keeping.
type Report struct {
	Version string
	Address string

	Cores            int
	MemoryTotalBytes int64
	DiskTotalBytes   int64

	CPUPercent  *float64
	MemoryBytes *int64
	DiskBytes   *int64

	// MeshNodeID is what the machine's own Engine says the swarm calls
	// it. Empty is a machine that is not on the cluster's network, and
	// the agent reports that rather than the control plane assuming it
	// from having sent the instructions.
	MeshNodeID string
}

// Desired is what the control plane tells a node to be running.
//
// **It is empty in this release**, and the shape is the point: the
// agent already asks the question on every pass and already applies the
// answer, so placing an app on a node is filling this in rather than
// inventing a way to reach the machine. A reconcile loop that starts
// life as a heartbeat is one that does not have to be replaced by one.
type Desired struct {
	// Apps are the containers this node should be running: one entry
	// per copy, so a machine running two of one app is told twice.
	Apps []Placement `json:"apps"`
}

// InMesh reports whether a machine is on the cluster's private network.
func (n *Node) InMesh() bool { return n.MeshNodeID != "" }

// Placement is one app a node should be running, and everything it
// takes to run it.
//
// It is a **complete instruction**, not a reference to look up: the
// machine has no database and no way to ask a second question. What
// travels is what `docker run` would need — an image, a login for the
// registry it is in, an environment, labels and networks — and the
// deployment id it belongs to, so the answer that comes back can be
// matched to what asked for it.
//
// The registry login is a real credential crossing the wire. It is the
// same one the control plane holds and it goes over the same TLS the
// agent authenticates through; there is no way for another machine to
// pull an image without one.
type Placement struct {
	// App is the full reference, for the agent's own log lines.
	App string `json:"app"`
	// Deploy is the deployment this placement is. The agent reports it
	// back untouched, which is what turns "it is running" into "that
	// deploy succeeded".
	Deploy int64 `json:"deploy"`
	// Ordinal tells this copy from the others of the same app on the
	// same machine. It starts at 1 and the machine reports it back
	// untouched, which is how the control plane knows which of them a
	// result is about.
	Ordinal int `json:"ordinal,omitempty"`
	// Container is the name to create. Chosen here so that the same
	// placement applied twice is the same container rather than a
	// second one — the agent asks "is this name running" and does
	// nothing when it is.
	Container string                `json:"container"`
	Image     string                `json:"image"`
	Registry  *dockerx.RegistryAuth `json:"registry,omitempty"`
	Env       map[string]string     `json:"env"`
	Labels    map[string]string     `json:"labels"`
	// Networks are what the container joins: the machine's own bridge
	// and, when there is one, the cluster's overlay.
	Networks []string `json:"networks"`
	// Resources is the ceiling this copy runs under: CPU quota and a
	// memory limit, zero in either meaning none.
	//
	// Sent on **every** pass rather than only when it changes, because
	// it is the one setting the Engine can write to a running
	// container — so the machine applying it every time is what makes
	// a limit raised on the control plane take effect here in the next
	// ten seconds instead of at the next deploy. An agent from before
	// this sends containers with no ceiling, which is what they had.
	Resources dockerx.Resources `json:"resources,omitzero"`
}

// Result is what a node did with a placement.
//
// One per placement it acted on, and only when something changed: a
// container that was already running is not news, and reporting it
// every ten seconds would be a deployment marked succeeded over and
// over for the life of the app.
type Result struct {
	App    string `json:"app"`
	Deploy int64  `json:"deploy"`
	// Ordinal is which copy of the app on this machine this is, echoed
	// back from the placement. An agent from before there could be more
	// than one sends none, and zero is read as the first — which is the
	// only copy such an agent can be running.
	Ordinal int `json:"ordinal,omitempty"`
	// Container is the id the machine created. Empty on a failure.
	Container string `json:"container_id"`
	// Error is why it did not run, in the machine's own words. Empty on
	// success.
	Error string `json:"error,omitempty"`
}

// Reading is what one container on a machine is using, as that machine
// measured it.
//
// A **percentage**, not the counters it came from: a CPU percentage is
// a difference between two readings, and only the machine holding the
// previous one can take it. Both sides compute it with
// metrics.CPUPercent, so a chart of a container on a worker is drawn on
// the same axis as one here — 100 is one core on either.
type Reading struct {
	// Container is the id the machine reported when it started it,
	// which is how the control plane works out whose reading this is.
	Container        string  `json:"container"`
	CPUPercent       float64 `json:"cpu_percent"`
	MemoryBytes      int64   `json:"memory_bytes"`
	MemoryLimitBytes int64   `json:"memory_limit_bytes"`
}

// Apps is the module that knows what runs on which machine.
//
// Declared here and satisfied by `app`, the same direction
// `metrics.Source` runs: this module owns the conversation with a
// machine and knows nothing about what an app is.
type Apps interface {
	// PlacementsFor is everything one machine should be running.
	PlacementsFor(ctx context.Context, nodeID int64) ([]Placement, error)
	// Placed records what it did with them.
	Placed(ctx context.Context, nodeID int64, results []Result) error
	// Sampled records what those containers are using.
	Sampled(ctx context.Context, nodeID int64, readings []Reading) error
	// Rebalance is told that the cluster has changed shape, so that
	// whatever follows it can be spread again.
	//
	// Declared here because this module is the one that knows when a
	// machine is added or taken away, and it is deliberately a
	// statement rather than a question: what "following the cluster"
	// means is `app`'s to decide, and this module does not know that
	// apps have a scale, an ordinal or a spread.
	//
	// It runs **before** a machine's row goes, which is what lets an
	// app leave a machine that is about to be deleted rather than
	// standing in the way of the delete — so `without` is that
	// machine's id, and it has to be excluded here rather than read
	// out of a table it is still in. Zero excludes nothing, which is
	// what adding a machine sends.
	Rebalance(ctx context.Context, without int64) error
}

var (
	// ErrNotFound is no such node.
	ErrNotFound = errors.New("no such server")

	// ErrAlreadyExists is a name already taken. Names are permanent, so
	// this is refused rather than resolved.
	ErrAlreadyExists = errors.New("a server with that name is already in this cluster")

	// ErrReservedSlug is a name this module's own API answers at.
	ErrReservedSlug = errors.New(`"agent" and "mesh" are reserved: one is where the workers call in, the other is what the cluster's network answers at`)

	// ErrControlPlane refuses an operation that only makes sense on a
	// worker. The control plane is not a machine this instance joined —
	// it is the instance.
	ErrControlPlane = errors.New("that is this machine, not a server it manages")

	// ErrNoAddress refuses to build a cluster this instance has no
	// address in. Every other machine has to reach this one, and the
	// only thing worse than not knowing where that is would be
	// advertising a bridge address — a swarm nothing can join, and no
	// way to tell from here that it cannot.
	ErrNoAddress = errors.New("this instance has no public address to build a cluster on: set one on the Instance screen, or give the machine a domain that resolves to it")

	// ErrNoContainer is asking about a container an app does not have.
	// A machine cannot read the log of something that never ran.
	ErrNoContainer = errors.New("that app has no container on its server")

	// ErrHasApps refuses to remove a machine something still runs on.
	// Where those apps should go is a decision, and making it by
	// deleting the row would make it invisibly.
	ErrHasApps = errors.New("apps are placed on that server: move them to another one first")

	// ErrUnknownToken is an agent presenting a credential that names no
	// node. Its own error rather than ErrNotFound: one is somebody
	// asking about a name, and this is a machine trying to authenticate.
	ErrUnknownToken = errors.New("that is not a credential of any server in this cluster")
)

// reservedSlugs are the names the API's own paths take under /nodes.
// Go's mux prefers a literal segment over a wildcard, so a node called
// one of these would be a machine nothing could open.
var reservedSlugs = map[string]bool{"agent": true, "mesh": true}
