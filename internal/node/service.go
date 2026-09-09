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

	// engine is what brings the cluster's private network up on this
	// machine. Nil on a daemon whose Docker cannot cluster and in every
	// test, and a cluster with no mesh is exactly that: machines that
	// report in and share no network.
	engine mesh.Engine
	// host is how the cluster's ports are opened on **this** machine.
	// The workers open their own; see internal/worker.
	host firewall.Host
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

func NewService(db *database.DB) *Service { return &Service{db: db} }

// SetMesh wires in what the cluster's network is made of. Called once,
// by server.New, with whatever this daemon actually has: a Docker that
// can cluster, a way to reach the host's firewall, and this instance's
// own address.
func (s *Service) SetMesh(engine mesh.Engine, host firewall.Host, advertise func(context.Context) string) {
	s.engine, s.host, s.advertise = engine, host, advertise
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
func (s *Service) Reconcile(ctx context.Context, n *Node, rep Report) (Desired, *mesh.Info, error) {
	if err := s.Repo().Record(ctx, n.ID, rep); err != nil {
		return Desired{}, nil, err
	}
	info, err := s.mesh(ctx)
	if err != nil {
		// A machine that called in is a machine that is up, whatever
		// the network is doing. It is told nothing about the mesh this
		// pass and asks again in ten seconds.
		log.Printf("cluster: %v", err)
		return Desired{Apps: []Placement{}}, nil, nil
	}
	if info != nil {
		if err := s.peers(ctx, n, info); err != nil {
			log.Printf("cluster: %v", err)
			return Desired{Apps: []Placement{}}, nil, nil
		}
	}
	return Desired{Apps: []Placement{}}, info, nil
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
