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
	"errors"
	"time"

	"cubeship/internal/user"
)

// ControlPlaneSlug names the row that is this machine. It is seeded by
// the migration and is not something anybody creates.
const ControlPlaneSlug = "control-plane"

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
	// Containers is how many this instance is running there.
	Containers int

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

	// Containers is how many containers of this instance's the agent
	// found running. It is what the control plane will later compare
	// against what it placed there.
	Containers int
}

// Desired is what the control plane tells a node to be running.
//
// **It is empty in this release**, and the shape is the point: the
// agent already asks the question on every pass and already applies the
// answer, so placing an app on a node is filling this in rather than
// inventing a way to reach the machine. A reconcile loop that starts
// life as a heartbeat is one that does not have to be replaced by one.
type Desired struct {
	// Apps are the containers this node should be running. Nothing puts
	// anything here yet.
	Apps []Placement `json:"apps"`
}

// Placement is one thing a node should be running. Reserved for the
// release that places apps; nothing constructs one yet.
type Placement struct{}

var (
	// ErrNotFound is no such node.
	ErrNotFound = errors.New("no such server")

	// ErrAlreadyExists is a name already taken. Names are permanent, so
	// this is refused rather than resolved.
	ErrAlreadyExists = errors.New("a server with that name is already in this cluster")

	// ErrReservedSlug is a name this module's own API answers at.
	ErrReservedSlug = errors.New(`"agent" is reserved: it is where the workers call in`)

	// ErrControlPlane refuses an operation that only makes sense on a
	// worker. The control plane is not a machine this instance joined —
	// it is the instance.
	ErrControlPlane = errors.New("that is this machine, not a server it manages")

	// ErrUnknownToken is an agent presenting a credential that names no
	// node. Its own error rather than ErrNotFound: one is somebody
	// asking about a name, and this is a machine trying to authenticate.
	ErrUnknownToken = errors.New("that is not a credential of any server in this cluster")
)

// reservedSlugs are the names the API's own paths take under /nodes.
// Go's mux prefers a literal segment over a wildcard, so a node called
// one of these would be a machine nothing could open.
var reservedSlugs = map[string]bool{"agent": true}
