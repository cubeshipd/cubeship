package dockerx

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
)

// The Engine's own clustering, used for one thing: an **overlay
// network**.
//
// Swarm mode is Docker's orchestrator — a scheduler, a distributed
// store, services, secrets — and Cubeship uses none of that. What it
// uses is the network: an overlay spans every machine in the swarm, and
// a container attached to one reaches, and resolves by name, a
// container on another box. That is the whole of what a cluster needs
// from the wire, and writing it by hand would be key management, subnet
// allocation, route programming and a name server.
//
// The scheduler is deliberately not used. Cubeship decides what runs
// where; `docker service` would be a second thing deciding that, with
// its own idea of health, restarts and rollbacks.

// SwarmState is what this Engine is, as far as clustering goes.
type SwarmState struct {
	// Active is whether this Engine is in a swarm at all.
	Active bool
	// Manager is whether it is one of the machines that decides. On a
	// Cubeship cluster exactly one is: the control plane.
	Manager bool
	// NodeID is what the swarm calls this machine. Empty when inactive.
	NodeID string
}

// Swarm reports whether this Engine is in a swarm, and as what.
func (c *Client) Swarm(ctx context.Context) (SwarmState, error) {
	info, err := c.api.Info(ctx)
	if err != nil {
		return SwarmState{}, fmt.Errorf("ask docker about its swarm: %w", err)
	}
	return SwarmState{
		Active:  info.Swarm.LocalNodeState == swarm.LocalNodeStateActive,
		Manager: info.Swarm.ControlAvailable,
		NodeID:  info.Swarm.NodeID,
	}, nil
}

// SwarmInit makes this Engine a manager of a new swarm.
//
// The advertise address is where the other machines will reach this one,
// and it has to be an address they can: a bridge address here would
// build a cluster nothing can join, which is the same trap
// settings.Routable exists for.
//
// Listening on all interfaces rather than on the advertised address
// alone, because the address a packet arrives on is not always the one
// the machine believes it has — a VPS behind a provider's NAT sees a
// private address on its own interface and a public one from outside.
func (c *Client) SwarmInit(ctx context.Context, advertise string) error {
	if advertise == "" {
		return fmt.Errorf("no address to advertise: the other machines would have nothing to join")
	}
	_, err := c.api.SwarmInit(ctx, swarm.InitRequest{
		ListenAddr:    "0.0.0.0:" + strconv.Itoa(SwarmManagerPort),
		AdvertiseAddr: advertise,
	})
	if err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("start a swarm: %w", err)
	}
	return nil
}

// SwarmWorkerToken is what a machine joining as a worker presents.
//
// It is a credential and it is not this instance's to keep: the Engine
// mints it, holds it, and hands it back on request, so there is nothing
// stored here that could go stale against a swarm somebody rotated.
func (c *Client) SwarmWorkerToken(ctx context.Context) (string, error) {
	s, err := c.api.SwarmInspect(ctx)
	if err != nil {
		return "", fmt.Errorf("read the swarm's join token: %w", err)
	}
	return s.JoinTokens.Worker, nil
}

// SwarmJoin puts this Engine in somebody else's swarm as a worker.
func (c *Client) SwarmJoin(ctx context.Context, manager, token, advertise string) error {
	if manager == "" || token == "" {
		return fmt.Errorf("no manager address or join token to join with")
	}
	err := c.api.SwarmJoin(ctx, swarm.JoinRequest{
		RemoteAddrs:   []string{manager},
		JoinToken:     token,
		ListenAddr:    "0.0.0.0:" + strconv.Itoa(SwarmManagerPort),
		AdvertiseAddr: advertise,
	})
	if err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("join the swarm at %s: %w", manager, err)
	}
	return nil
}

// SwarmManagerPort is where a manager accepts joins. It is Docker's own
// default and is not configurable here: it is one of the ports the
// cluster's firewall rules are written for, and a number that could
// differ per machine would be a rule that has to be worked out per
// machine.
const SwarmManagerPort = 2377

// SwarmManagerAddress is the address a worker joins at.
func SwarmManagerAddress(host string) string {
	return net.JoinHostPort(host, strconv.Itoa(SwarmManagerPort))
}

// EnsureOverlayNetwork creates the network every machine's containers
// attach to, or leaves the one that is there.
//
// **Attachable**, which is the whole reason this is usable here: an
// overlay is otherwise for `docker service` tasks only, and attachable
// is what lets an ordinary container — which is all Cubeship creates —
// join one.
//
// It can only be created on a manager. The workers do not create it and
// do not have to: an overlay reaches a machine when a container there
// attaches to it.
func (c *Client) EnsureOverlayNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, types.NetworkCreate{
		Driver:     "overlay",
		Attachable: true,
	}); err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create the overlay network %q: %w", name, err)
	}
	return nil
}
