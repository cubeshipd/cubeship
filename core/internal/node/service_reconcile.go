package node

import (
	"context"
	"errors"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// rebalance tells the module that owns apps that the set of machines has
// changed. Best effort and never fatal — see the callers.
func (s *Service) rebalance(ctx context.Context, without int64, why string) {
	if s.apps == nil {
		return
	}
	if err := s.apps.Rebalance(ctx, without); err != nil {
		log.Printf("node: re-spreading the apps that follow the cluster after %s: %v", why, err)
	}
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

// MeshStatus is what the cluster's private network is, and whether it
// carries its traffic encrypted.
//
// Its own answer rather than a field on every machine, because it is a
// fact about the network and not about any box on it.
//
// **Unencrypted is worth reporting rather than assuming.** Docker fixes
// the flag when the overlay is created and offers no way to change it,
// so an instance whose mesh came up before this instance asked for one
// has a network that carries every app request between machines in the
// clear — and nothing about that is visible from outside. What fixes it
// is removing the network, which is not something to do behind
// somebody's back: every container on it loses the cluster's network
// until it is created again.
func (s *Service) MeshStatus(ctx context.Context, caller *user.User) (Mesh, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelView, ""); err != nil {
		return Mesh{}, err
	}
	if s.engine == nil {
		return Mesh{}, nil
	}
	found, err := s.engine.NetworkExists(ctx, mesh.NetworkName)
	if err != nil || !found {
		return Mesh{}, err
	}
	encrypted, err := mesh.Encrypted(ctx, s.engine)
	if err != nil {
		return Mesh{}, err
	}
	return Mesh{Network: mesh.NetworkName, Encrypted: encrypted}, nil
}

// Mesh is the cluster's private network as this instance finds it.
type Mesh struct {
	// Network is the overlay's name, empty on an instance that has no
	// cluster — which is one that never added a second machine.
	Network string
	// Encrypted is whether what crosses between machines is carried
	// over IPsec. Always false when there is no network.
	Encrypted bool
}

// List is the cluster, this machine included.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Node, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelView, ""); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Get is one machine by name.
func (s *Service) Get(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelView, ""); err != nil {
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
	if err := user.Allow(caller, user.ResServers, user.LevelManage, ""); err != nil {
		return nil, err
	}
	n, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, err
	}
	if n.ControlPlane {
		return nil, ErrControlPlane
	}
	// Before the row goes, so that an app which follows the cluster
	// leaves this machine rather than standing in the way of the
	// delete. An app somebody placed here by hand still refuses the
	// delete, and that is the difference: where it should go is a
	// decision, and making it by deleting a row would make it
	// invisibly.
	s.rebalance(ctx, n.ID, "removing "+name)
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

	if rep.Host != nil {
		if err := machine.RecordTelemetry(ctx, s.db, n.ID, *rep.Host); err != nil {
			log.Printf("cluster: recording host metrics for %s: %v", n.Slug, err)
		}
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

// Workers is every machine that is not this one, by id, with the
// version each last reported.
//
// The control plane is left out because it is not something this
// instance tells to update — it is the thing doing the updating, and it
// goes last, by replacing itself.
//
// A machine that has never called in reports an empty version, which
// reads as "not on the version we want" and so is told. It will not
// hear, and that is fine: a machine that does not answer is carried on
// without, and gets the new version whenever it does return.
func (s *Service) Workers(ctx context.Context) (map[int64]string, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(all))
	for _, n := range all {
		if !n.ControlPlane {
			out[n.ID] = n.Version
		}
	}
	return out, nil
}

// Update tells one machine to replace itself with a version.
//
// It returns as soon as the machine has been told. See CommandUpdate:
// there is no answer to wait for, because answering would mean
// surviving the thing it was told to do.
func (s *Service) Update(_ context.Context, nodeID int64, version string) error {
	return s.hub.Tell(nodeID, Command{Kind: CommandUpdate, Version: version})
}

func (s *Service) ResolveMachine(ctx context.Context, caller *user.User, name string) (int64, bool, error) {
	n, err := s.Get(ctx, caller, name)
	if errors.Is(err, ErrNotFound) {
		return 0, false, database.ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return n.ID, n.ControlPlane, nil
}

// Request uses the existing outbound-only worker channel for a bounded read.
func (s *Service) Request(ctx context.Context, id int64, command Command) ([]byte, error) {
	return s.hub.Ask(ctx, id, command)
}
