// Package mesh is the private network the machines in a cluster share.
//
// It is **Docker's own overlay network**, and nothing else about Docker
// Swarm. Swarm mode is an orchestrator — a scheduler, a store,
// services, secrets — and Cubeship uses none of that: it decides what
// runs where itself, and a second thing deciding that would be two
// answers to one question. What it takes is the wire.
//
// The trade that bought it is worth writing down, because it was argued
// over. The alternative was WireGuard, coordinated here: a keypair and
// a subnet per machine, peers distributed down the agent's loop, routes
// programmed on each box. That is a tunnel and no more — Docker's
// embedded DNS is per-daemon, so **names would not resolve across
// machines** and every address would have to be worked out here and
// injected. An overlay gives connectivity and name resolution together,
// so `cubeship-db-pg` means the same thing on every machine in the
// cluster, and none of the modules that address a container by name had
// to learn where it is.
//
// What it costs is ports open between the machines, and that is why
// this package writes firewall rules as well as joining swarms. They
// are scoped to the peers' own addresses — which the control plane
// knows, because every agent reports one — so what is open is open to
// the cluster rather than to the internet.
//
// **A machine behind NAT cannot be in the mesh.** The data plane is
// VXLAN between the nodes themselves, so they have to reach each other
// directly. That is a real limit and it is the one this design pays for
// everything else with.
package mesh

import (
	"context"
	"fmt"

	"cubeship/internal/firewall"
	"cubeship/internal/platform/dockerx"
)

// NetworkName is the overlay every container in the cluster attaches
// to.
//
// A **second** network beside `cubeship`, not a replacement for it.
// Converting the bridge in place is not a thing Docker can do — the
// network would have to be removed, which means disconnecting every
// container on it — so an instance that adds a machine would have to
// take everything down to gain a network it is not yet using. Beside
// it, nothing existing is touched, and a container joins the mesh the
// next time it is created, which is the same rule that already governs
// its labels and its environment.
const NetworkName = "cubeship-mesh"

// Ports are what swarm needs open between the machines.
//
// Three of them, and each is a different job: 2377 is where a machine
// joins, and only a manager listens on it; 7946 is the gossip that
// carries which container is where; 4789 is the VXLAN the traffic
// itself goes over. All three are the host's own — nothing here is a
// published container port — so they are `host` scope, and none of this
// needs the DOCKER-USER adoption an `apps` rule would.
var Ports = []Port{
	{Number: dockerx.SwarmManagerPort, Protocol: firewall.ProtocolTCP, Managers: true,
		Why: "cubeship cluster: joins"},
	{Number: 7946, Protocol: firewall.ProtocolTCP, Why: "cubeship cluster: gossip"},
	{Number: 7946, Protocol: firewall.ProtocolUDP, Why: "cubeship cluster: gossip"},
	{Number: 4789, Protocol: firewall.ProtocolUDP, Why: "cubeship cluster: traffic"},
}

// Port is one of them.
type Port struct {
	Number   int
	Protocol firewall.Protocol
	// Managers is whether only the machine that decides listens here.
	// Opening a port nothing is listening on is not dangerous, but it
	// is a rule that says something untrue about the machine.
	Managers bool
	Why      string
}

// Info is everything a machine needs to be on the mesh, as the control
// plane hands it down.
type Info struct {
	// Manager is the address a worker joins at, host:port.
	Manager string `json:"manager"`
	// JoinToken is the swarm's own credential, read from the Engine on
	// every pass rather than stored: it is Docker's to mint and to
	// rotate, and a copy here would be one that goes stale silently.
	JoinToken string `json:"join_token"`
	// Network is the overlay's name, so the agent does not have to
	// agree with this package at compile time about it.
	Network string `json:"network"`
	// Peers are the addresses of the **other** machines, which is what
	// this one's firewall has to admit.
	Peers []string `json:"peers"`
}

// Engine is what this package needs from Docker. *dockerx.Client
// satisfies it; a test supplies a fake.
type Engine interface {
	Swarm(ctx context.Context) (dockerx.SwarmState, error)
	SwarmInit(ctx context.Context, advertise string) error
	SwarmWorkerToken(ctx context.Context) (string, error)
	SwarmJoin(ctx context.Context, manager, token, advertise string) error
	EnsureOverlayNetwork(ctx context.Context, name string) error
	NetworkExists(ctx context.Context, name string) (bool, error)
}

// Ensure brings the mesh up on the control plane and reports what a
// machine needs to join it.
//
// Called when there is a machine to join it and not before: an instance
// that never adds a second box never becomes a swarm manager, for the
// same reason it never starts BuildKit. Idempotent — a swarm that
// exists is left alone, and so is the network.
func Ensure(ctx context.Context, engine Engine, advertise string) (Info, error) {
	// Refused here rather than left to Docker, because what a swarm
	// advertises is the one field in it that cannot be corrected later
	// without tearing the cluster down: every machine joins at the
	// address the manager published.
	if advertise == "" {
		return Info{}, fmt.Errorf("no address to advertise: the other machines would have nothing to join")
	}
	state, err := engine.Swarm(ctx)
	if err != nil {
		return Info{}, err
	}
	switch {
	case !state.Active:
		if err := engine.SwarmInit(ctx, advertise); err != nil {
			return Info{}, err
		}
	case !state.Manager:
		// This Engine is in somebody's swarm as a worker. Cubeship did
		// not do that — a control plane is the manager of its own — so
		// it is left exactly as it is rather than being torn out of
		// whatever it belongs to.
		return Info{}, fmt.Errorf("this machine's Docker is already a worker in another swarm; leave it (docker swarm leave) before this instance can run a cluster")
	}

	if err := engine.EnsureOverlayNetwork(ctx, NetworkName); err != nil {
		return Info{}, err
	}
	token, err := engine.SwarmWorkerToken(ctx)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Manager:   dockerx.SwarmManagerAddress(advertise),
		JoinToken: token,
		Network:   NetworkName,
	}, nil
}

// Join puts this machine in the cluster's swarm, or leaves it where it
// already is.
//
// The overlay is not created here. It belongs to the manager, and it
// reaches a machine when a container there attaches to it.
func Join(ctx context.Context, engine Engine, info Info, advertise string) error {
	state, err := engine.Swarm(ctx)
	if err != nil {
		return err
	}
	if state.Active {
		return nil
	}
	return engine.SwarmJoin(ctx, info.Manager, info.JoinToken, advertise)
}

// Admit opens the cluster's ports to the peers, and to nobody else.
//
// **Added, never removed.** A rule admitting a machine that has left is
// a port open to an address that was in this cluster, which is a small
// thing; a rule removed while the cluster still needs it is a machine
// that drops out of the network, which is not. Removing one is the
// operator's, on the firewall screen, where every rule this writes
// appears like any other — they are ufw's rules, and this module owns
// no copy of them.
//
// Best effort by design: a host with no ufw is a host with nothing
// blocking these ports, and a firewall this daemon cannot reach is one
// somebody else is managing. Neither is a reason to refuse to cluster.
func Admit(ctx context.Context, host firewall.Host, peers []string, manager bool) error {
	if host == nil || !host.Available() {
		return nil
	}
	for _, peer := range peers {
		for _, port := range Ports {
			if port.Managers && !manager {
				continue
			}
			spec := firewall.Spec{
				Scope:    firewall.ScopeHost,
				Action:   firewall.ActionAllow,
				Protocol: port.Protocol,
				Port:     fmt.Sprintf("%d", port.Number),
				From:     peer,
				Comment:  port.Why,
			}
			if err := spec.Check(); err != nil {
				// A peer that is not an address is a machine that has
				// not reported one. Skipping it is right: the rule
				// would be a guess, and nothing user-supplied reaches
				// a command line here.
				break
			}
			if err := firewall.Add(ctx, host, spec); err != nil {
				return fmt.Errorf("admit %s on %d: %w", peer, port.Number, err)
			}
		}
	}
	return nil
}
