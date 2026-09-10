package node

import (
	"context"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"cubeship/internal/firewall"
	"cubeship/internal/mesh"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service is the cluster: which machines are in it, and what the
// workers are told when they call.
//
// It sits at the bottom with `user` and knows about no other module.
// What runs *on* a node is the placing module's business, and it will
// reach this one through the Desired the agent already asks for.
type Service struct {
	db *database.DB

	// hub is what lets this instance ask a machine something. See
	// commands.go: the answer to a machine's own poll is the only way
	// in, because nothing here can dial one.
	hub *hub

	// engine is what brings the cluster's private network up on this
	// machine. Nil on a daemon whose Docker cannot cluster and in every
	// test, and a cluster with no mesh is exactly that: machines that
	// report in and share no network.
	engine mesh.Engine
	// host is how the cluster's ports are opened on **this** machine.
	// The workers open their own; see internal/worker.
	host firewall.Host
	// apps knows what should be running where. Nil until server.New
	// wires it in, and then a cluster is machines that report in and
	// are told to run nothing.
	apps Apps
	// registry answers where this instance's own registry is, so a
	// machine knows which images it should authenticate as itself for.
	registry func(ctx context.Context) string
	// edge answers what a machine's own Traefik is started with.
	edge func(ctx context.Context) Edge
	// advertise is where the other machines reach this one. It is the
	// instance's own public address, which settings already works out
	// and refuses to guess badly — a bridge address here would build a
	// cluster nothing can join.
	advertise func(ctx context.Context) string

	// admitted is the peer set this machine's firewall was last opened
	// for. Writing a ufw rule costs a container through hostexec, so it
	// is done when the cluster changes rather than on every pass.
	mu       sync.Mutex
	admitted string
	// networkAt is when the Engine was last asked whether the cluster's
	// overlay is there. Every container this instance creates asks, and
	// the answer changes about once in the life of an instance.
	networkAt   time.Time
	networkName string
}

// meshLookupTTL is how long "is there a cluster network" is believed.
//
// The question is asked on every container this instance creates, and
// the answer changes when somebody adds the first machine — so it is
// worth not asking the Engine per deploy, and not worth a subscription.
const meshLookupTTL = 30 * time.Second

func NewService(db *database.DB) *Service { return &Service{db: db, hub: newHub()} }

// SetMesh wires in what the cluster's network is made of. Called once,
// by server.New, with whatever this daemon actually has: a Docker that
// can cluster, a way to reach the host's firewall, and this instance's
// own address.
func (s *Service) SetMesh(engine mesh.Engine, host firewall.Host, advertise func(context.Context) string) {
	s.engine, s.host, s.advertise = engine, host, advertise
}

// SetApps wires in what knows which apps belong on which machine.
// Called once, by server.New — the module that owns apps sits above
// this one, so it is handed back down here.
func (s *Service) SetApps(a Apps) { s.apps = a }

// SetRegistryHost wires in where this instance's own registry answers.
// It follows the instance's domain, so it is asked rather than captured.
func (s *Service) SetRegistryHost(fn func(context.Context) string) { s.registry = fn }

// SetEdgeConfig wires in what a machine's own Traefik is started with.
// It follows the instance's settings, so it is asked rather than
// captured.
func (s *Service) SetEdgeConfig(fn func(context.Context) Edge) { s.edge = fn }

// EdgeConfig is what every machine's own Traefik is started with, or
// nil on a daemon that does not know — which is a test.
func (s *Service) EdgeConfig(ctx context.Context) *Edge {
	if s.edge == nil {
		return nil
	}
	edge := s.edge(ctx)
	return &edge
}

// RegistryHost is the address an image pushed to this instance is
// pulled from, or empty when there is no domain and therefore no
// registry.
func (s *Service) RegistryHost(ctx context.Context) string {
	if s.registry == nil {
		return ""
	}
	return s.registry(ctx)
}

// Logs is the tail of a container's log, read on the machine it is on.
//
// It is the first thing that goes through the reverse channel, and the
// shape is the point: the request parks here, the machine's next poll
// carries the command, and its answer releases this. What comes back is
// bytes, already demultiplexed out of Docker's frame format by the
// machine that read them — so what a caller does with this is what it
// does with a local log, and internal/app does not branch on which
// machine an app is on beyond choosing this door.
func (s *Service) Logs(ctx context.Context, nodeID int64, containerID, tail string) ([]byte, error) {
	if containerID == "" {
		return nil, ErrNoContainer
	}
	return s.hub.Ask(ctx, nodeID, Command{
		Kind: CommandLogs, Container: containerID, Tail: tail,
	})
}

// Addresses is where each machine in the cluster is reached, by id.
//
// One query rather than one per app: what asks is a listing, and a
// listing that resolved a machine per row would be a listing that gets
// slower as the cluster grows. A machine that has not reported an
// address is not in the answer at all — there is nothing to point a
// name at, and an empty string would read as one.
func (s *Service) Addresses(ctx context.Context) (map[int64]string, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(all))
	for _, n := range all {
		if n.Address != "" {
			out[n.ID] = n.Address
		}
	}
	return out, nil
}

// Quiet is the machines that have not called in for at least d, by id.
//
// The caller says how patient it wants to be, because the two questions
// are not the same: three missed passes is enough to stop *routing* to
// a machine and nowhere near enough to conclude that something waiting
// on it is never going to happen. `unreachable` is the first; this is
// how the second is asked.
//
// Derived from last_seen_at on every read, like Node.Status itself, so
// it cannot go stale the way a stored column would. Only the machines
// that are actually quiet are in it: a caller reads a missing key as
// "it is answering", which is what an instance with no cluster gets.
//
// A machine that has **never** called in is quiet however long d is —
// it is a row somebody made and never installed, and something waiting
// on it is waiting for a box that does not exist.
//
// The control plane is never in the answer. It cannot be out of touch
// with itself, and a daemon reporting its own box missing would be one
// deciding something is wrong from the only place that can be sure it
// is not.
func (s *Service) Quiet(ctx context.Context, d time.Duration) (map[int64]bool, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, n := range all {
		switch {
		case n.ControlPlane:
		case n.LastSeenAt == nil, time.Since(*n.LastSeenAt) >= d:
			out[n.ID] = true
		}
	}
	return out, nil
}

// Wake tells a machine there is something new for it, without waiting
// for anything back.
//
// What calls it is a deploy placed on that machine: the poll it releases
// is the difference between a deploy starting now and one starting when
// the machine next asks.
func (s *Service) Wake(nodeID int64) { s.hub.Signal(nodeID) }

// AuthenticateNode turns a machine's credential into its name.
//
// It is the registry's seam: a worker pulls the images this instance
// holds, and what it presents is the credential it already
// authenticates its own loop with. Returning the name rather than the
// node keeps the registry knowing nothing about what one is.
func (s *Service) AuthenticateNode(ctx context.Context, token string) (string, error) {
	n, err := s.Authenticate(ctx, token)
	if err != nil {
		return "", err
	}
	return n.Slug, nil
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Add puts a machine in the cluster and mints the credential its agent
// will authenticate with.
//
// **The credential is returned once and cannot be read again**, like an
// API key: only its hash is stored. Losing it means removing the node
// and adding it back, which is the same cost as rotating it and one
// fewer thing to build.
//
// Nothing is contacted. The row is a place for a machine that does not
// exist yet — somebody now goes and runs the installer on it — and a
// node that has never called is `pending` rather than an error.
func (s *Service) Add(ctx context.Context, caller *user.User, name, description string) (*Node, string, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, "", err
	}
	if err := checkSlug(name); err != nil {
		return nil, "", err
	}
	token, err := authkey.Generate()
	if err != nil {
		return nil, "", fmt.Errorf("generate the server's credential: %w", err)
	}
	// The cluster's network comes up now rather than when the machine
	// first calls, because this is the moment somebody is watching. A
	// swarm that cannot start — no public address to advertise, a
	// Docker already in somebody else's swarm — is a machine that would
	// join and never reach anything, and finding that out from a
	// refusal here beats finding it out from a screen that says `ready`.
	if _, err := s.mesh(ctx); err != nil {
		return nil, "", err
	}

	created, err := s.Repo().Create(ctx, name, description, authkey.Hash(token))
	if err != nil {
		return nil, "", err
	}
	return created, token, nil
}

// mesh brings the cluster's private network up on this machine and
// reports what a worker needs to join it.
//
// Idempotent and cheap in the steady state — the Engine is asked what
// it is, the overlay is created-or-not, and the swarm's own join token
// is read back rather than stored. Nil, with no error, on a daemon that
// has no Docker to cluster with: that is a test, and a cluster with no
// mesh is machines that report in and share no network.
func (s *Service) mesh(ctx context.Context) (*mesh.Info, error) {
	if s.engine == nil {
		return nil, nil
	}
	advertise := ""
	if s.advertise != nil {
		advertise = s.advertise(ctx)
	}
	if advertise == "" {
		return nil, ErrNoAddress
	}
	info, err := mesh.Ensure(ctx, s.engine, advertise)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// MeshNetwork is the overlay a container on this machine should join,
// or empty when this instance is one machine.
//
// Every module that creates a container asks, and takes the answer as
// an extra network rather than as a replacement for the local bridge:
// what is on this box already resolves it there, and the overlay is
// what the other machines resolve it on.
//
// The Engine is the authority rather than this module's own state. The
// network can be removed by hand, an instance can be upgraded into a
// cluster that already exists, and a daemon restart forgets everything
// but the table — asking the thing that would have to answer anyway is
// the answer that cannot drift.
func (s *Service) MeshNetwork(ctx context.Context) string {
	if s.engine == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.networkAt) < meshLookupTTL {
		return s.networkName
	}
	name := ""
	if found, err := s.engine.NetworkExists(ctx, mesh.NetworkName); err == nil && found {
		name = mesh.NetworkName
	}
	s.networkAt, s.networkName = time.Now(), name
	return name
}

// List is the cluster, this machine included.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Node, error) {
	if err := user.Require(caller, RoleToRead); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Get is one machine by name.
func (s *Service) Get(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if err := user.Require(caller, RoleToRead); err != nil {
		return nil, err
	}
	return s.Repo().BySlug(ctx, name)
}

// Remove takes a machine out of the cluster.
//
// **It is a local act.** The row goes and the credential with it, so
// the agent's next call is refused and the worker stops being told
// anything — but nothing reaches out to that box, and whatever is
// running on it goes on running until somebody stops it. That is the
// honest shape for a machine that may be unreachable, off, or gone:
// the alternative is a delete that hangs on a host nobody can dial.
//
// The control plane cannot be removed. It is not a machine this
// instance joined; it is the instance.
func (s *Service) Remove(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, err
	}
	n, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, err
	}
	if n.ControlPlane {
		return nil, ErrControlPlane
	}
	if err := s.Repo().Delete(ctx, n.ID); err != nil {
		return nil, err
	}
	return n, nil
}

// Authenticate turns an agent's credential into the node it belongs to.
//
// It takes no caller and answers no question about a person: a node is
// not one. This is the whole of the agent surface's authorization —
// holding the credential *is* being that machine, and every reconcile
// is scoped to the node it resolved to.
func (s *Service) Authenticate(ctx context.Context, token string) (*Node, error) {
	if token == "" {
		return nil, ErrUnknownToken
	}
	n, err := s.Repo().ByTokenHash(ctx, authkey.Hash(token))
	if err != nil {
		return nil, err
	}
	// A credential that resolves to the control plane would mean this
	// machine dialling itself. Nothing mints one — the row's token is
	// NULL — so this is a belt-and-braces refusal rather than a case
	// anybody can reach.
	if n.ControlPlane {
		return nil, ErrUnknownToken
	}
	return n, nil
}

// Reconcile is one pass of the agent loop: the machine says what it is,
// and is told what it should be running.
//
// The report is recorded before the answer is built, so a node that
// calls is marked seen even if working out its desired state fails.
// Being seen is a fact about the network; what it should run is a
// question about this instance, and one must not be lost with the
// other.
//
// **The answer is empty in this release.** See Desired: the loop exists
// now so that placing an app on a node is filling it in rather than
// inventing a way to reach the machine.
func (s *Service) Reconcile(ctx context.Context, n *Node, rep Report, results []Result, readings []Reading) (Desired, *mesh.Info, error) {
	if err := s.Repo().Record(ctx, n.ID, rep); err != nil {
		return Desired{}, nil, err
	}

	// What the machine did comes first. A deploy it has just finished is
	// what decides which placement it is told about next, and reading
	// them the other way round would tell it to run the version it has
	// already replaced.
	if s.apps != nil && len(results) > 0 {
		if err := s.apps.Placed(ctx, n.ID, results); err != nil {
			log.Printf("cluster: recording what %s did: %v", n.Slug, err)
		}
	}
	// What those containers are using, which is the machine's to
	// measure: nothing here can reach its Engine. Recorded before the
	// answer is built for the same reason the results are — being seen
	// is a fact, and losing it because working out the next instruction
	// failed would be losing it for nothing.
	if s.apps != nil && len(readings) > 0 {
		if err := s.apps.Sampled(ctx, n.ID, readings); err != nil {
			log.Printf("cluster: recording what %s is using: %v", n.Slug, err)
		}
	}
	desired := s.Desired(ctx, n)
	info, err := s.mesh(ctx)
	if err != nil {
		// A machine that called in is a machine that is up, whatever
		// the network is doing. It is told nothing about the mesh this
		// pass and asks again in ten seconds.
		log.Printf("cluster: %v", err)
		return desired, nil, nil
	}
	if info != nil {
		if err := s.peers(ctx, n, info); err != nil {
			log.Printf("cluster: %v", err)
			return desired, nil, nil
		}
	}
	return desired, info, nil
}

// Desired is what a machine should be running, and nothing else: no
// report is recorded and nothing is written.
//
// Separate from Reconcile because a poll that parks and is then woken
// has to ask this question again — the answer may have changed while it
// waited, which is the whole reason it was woken — and asking it by
// calling Reconcile again would stamp the machine as having reported an
// empty pass, overwriting what it actually said with zeroes.
func (s *Service) Desired(ctx context.Context, n *Node) Desired {
	desired := Desired{Apps: []Placement{}}
	if s.apps == nil {
		return desired
	}
	apps, err := s.apps.PlacementsFor(ctx, n.ID)
	if err != nil {
		log.Printf("cluster: working out what %s should run: %v", n.Slug, err)
		return desired
	}
	if apps != nil {
		desired.Apps = apps
	}
	// What its edge serves that its own containers do not say. A
	// machine that cannot be told this still runs everything on it —
	// the containers and their labels are the other half of the answer
	// and are already above — so a failure here is logged and the pass
	// goes on rather than leaving the machine with nothing.
	routes, err := s.apps.RoutesFor(ctx, n.ID)
	if err != nil {
		log.Printf("cluster: working out what %s should serve: %v", n.Slug, err)
		return desired
	}
	desired.Routes = routes
	return desired
}

// peers fills in who the machine being answered has to admit, and opens
// this machine's own firewall to the machines that have called in.
//
// Both halves are the same fact read in two directions: every node in
// the cluster with an address is a peer of every other. The control
// plane's own address is what it advertises, which is why its row is
// kept up to date here rather than by an agent it does not have.
func (s *Service) peers(ctx context.Context, answering *Node, info *mesh.Info) error {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return err
	}
	// The manager's address without the port. net.SplitHostPort rather
	// than a Cut on the colon, because an IPv6 address is full of them
	// and is bracketed for exactly that reason.
	advertise := info.Manager
	if host, _, err := net.SplitHostPort(info.Manager); err == nil {
		advertise = host
	}

	var workers []string
	for _, other := range all {
		if other.ControlPlane {
			// Its row carries what it advertises and what the swarm
			// calls it, so the cluster screen can say both without the
			// machine having an agent to report them.
			if other.Address != advertise || other.MeshNodeID == "" {
				if state, err := s.engine.Swarm(ctx); err == nil {
					_ = s.Repo().RecordMesh(ctx, other.ID, advertise, state.NodeID)
				}
			}
			continue
		}
		if other.Address == "" {
			continue
		}
		workers = append(workers, other.Address)
		if other.ID != answering.ID {
			info.Peers = append(info.Peers, other.Address)
		}
	}
	// Every worker is answered with the manager as a peer too: it is
	// the machine it gossips with, and the one it joined through.
	info.Peers = append(info.Peers, advertise)

	return s.admit(ctx, workers)
}

// admit opens this machine's ports to the machines in the cluster, when
// the set of them has changed.
//
// Guarded by what was last opened because each rule is a container
// through hostexec: doing this on every pass would be a dozen
// throwaway containers a minute for a firewall that already says what
// it needs to.
func (s *Service) admit(ctx context.Context, peers []string) error {
	sort.Strings(peers)
	key := strings.Join(peers, ",")

	s.mu.Lock()
	unchanged := key == s.admitted
	s.mu.Unlock()
	if unchanged {
		return nil
	}
	if err := mesh.Admit(ctx, s.host, peers, true); err != nil {
		return err
	}
	s.mu.Lock()
	s.admitted = key
	s.mu.Unlock()
	return nil
}

func checkSlug(name string) error {
	if slug.Reserved(name) {
		return slug.ErrReserved
	}
	if reservedSlugs[name] {
		return ErrReservedSlug
	}
	if name == ControlPlaneSlug {
		return ErrControlPlane
	}
	if !slug.Valid(name) {
		return slug.ErrInvalid
	}
	return nil
}
