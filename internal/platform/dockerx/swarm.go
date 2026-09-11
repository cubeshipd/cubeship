package dockerx

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/errdefs"
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

// NetworkEncrypted reports whether an overlay carries its traffic over
// IPsec. False for a network that does not exist, and for one created
// before this instance asked for it — the flag is set when the network
// is made and Docker offers no way to change it after.
func (c *Client) NetworkEncrypted(ctx context.Context, name string) (bool, error) {
	n, err := c.api.NetworkInspect(ctx, name, network.InspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect network %q: %w", name, err)
	}
	_, on := n.Options["encrypted"]
	return on, nil
}

// NetworkExists reports whether a network is there to be joined.
//
// It is how the daemon decides whether to put a container on the
// cluster's overlay: an instance of one machine has no such network,
// and asking for it would be a container that will not start.
func (c *Client) NetworkExists(ctx context.Context, name string) (bool, error) {
	if _, err := c.api.NetworkInspect(ctx, name, network.InspectOptions{}); err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("look for network %q: %w", name, err)
	}
	return true, nil
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
//
// **Encrypted**, and that is not a precaution about internal traffic —
// it is the traffic. Every name this instance serves arrives at the
// control plane and is proxied to a container that may be on another
// machine, and TLS ends at the proxy: what crosses the wire between two
// boxes is plain HTTP with its Authorization headers and its session
// cookies in it, plus every connection an app on one machine makes to a
// database on another. Between two VPS that is the provider's network
// and quite possibly the open internet.
//
// What it costs is small and worth naming so nobody has to guess: IPsec
// ESP with AES-GCM, in the kernel, on hardware with AES-NI — which is
// every server CPU of the last fifteen years. The cost is proportional
// to bytes rather than to packets, and the VXLAN encapsulation this
// rides on already costs more per packet than encrypting its contents
// does.
//
// **It is fixed when the network is created.** Docker has no way to
// turn it on for an overlay that exists, so an instance whose mesh came
// up before this keeps an unencrypted one until that network is
// removed — see mesh.Status, which is what says so rather than leaving
// somebody to assume.
func (c *Client) EnsureOverlayNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		// Docker reads the presence of the key, not its value.
		Options: map[string]string{"encrypted": ""},
	}); err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create the overlay network %q: %w", name, err)
	}
	return nil
}
