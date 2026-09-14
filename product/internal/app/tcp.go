package app

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// TCPPort is a port of an app's container published on a host port of the
// control plane, for a protocol that is not HTTP: SSH into a Git server, a
// game server, a broker. Traefik routes HTTP by name, and these carry no
// name to route by, so the container publishes the port itself — the way an
// exposed datastore does.
//
// **It pins the app, like a volume.** A host port is bound by one container,
// so the app runs as one copy and its deploys stop the old container before
// the new one starts: the new one could not bind the port while the old one
// holds it. **And on the control plane**, because that is the machine the
// instance's domains point at and the one whose firewall this instance
// writes. See Orchestrator.swapInPlace.
type TCPPort struct {
	ID    int64
	AppID int64
	// ContainerPort is what the app listens on inside its container.
	ContainerPort int
	// HostPort is where the control plane publishes it.
	HostPort  int
	CreatedAt time.Time
}

const (
	// TCPPortRangeStart and TCPPortRangeEnd are where a host port is picked
	// when none is named. Past the datastores' 15000s and the stores'
	// 16000s, so an automatic pick on either side never meets the other.
	TCPPortRangeStart = 17000
	TCPPortRangeEnd   = 17999
	// MinTCPHostPort is the lowest port that may be named, for the
	// datastores' reason: the numbers below are where the host's own
	// services already are, 22 among them.
	MinTCPHostPort = 1024
)

var (
	ErrInvalidTCPPort  = fmt.Errorf("a container port is 1-65535, and a host port is %d-65535, or left out to pick one", MinTCPHostPort)
	ErrTCPPortExists   = errors.New("this app already publishes that container port")
	ErrTCPPortTaken    = errors.New("that host port is already published by something on this instance")
	ErrTCPPortNotFound = errors.New("no such TCP port on this app")
	ErrNoTCPPortsLeft  = fmt.Errorf("no free port left in the range %d-%d; name one explicitly", TCPPortRangeStart, TCPPortRangeEnd)
	// ErrTCPNeedsOneCopy is publishing a port of an app that runs as more
	// than one copy, anywhere but the control plane, or chooses its count.
	ErrTCPNeedsOneCopy = errors.New("a TCP port needs the app to run as one copy on the control plane, with spread and autoscaling off")
	// ErrTCPPinsApp is the other direction: changing where an app with a
	// published port runs, or how many of it.
	ErrTCPPinsApp = errors.New("this app publishes a TCP port, so it runs as one copy on the control plane, where the port is; remove its TCP ports to place or scale it")
)

// validTCPPorts reports whether a container port and a named host port are
// ones this instance publishes. A host port of 0 is "pick one".
func validTCPPorts(containerPort, hostPort int) bool {
	if containerPort < 1 || containerPort > 65535 {
		return false
	}
	return hostPort == 0 || (hostPort >= MinTCPHostPort && hostPort <= 65535)
}

// pickTCPPort is the first host port in the range nothing uses.
func pickTCPPort(used map[int]bool) (int, error) {
	for port := TCPPortRangeStart; port <= TCPPortRangeEnd; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, ErrNoTCPPortsLeft
}

// canPublishTCP reports whether an app is in the shape a published port
// needs: one copy, on the control plane.
func canPublishTCP(a *App, controlPlane int64) bool {
	return len(a.Replicas) == 1 && a.Replicas[0].NodeID == controlPlane &&
		!a.Spread && !a.Autoscale.On() && a.Scale <= 1
}

// keepsTCP reports whether a placement leaves an app with a published port
// as one copy where it is, which is the control plane.
func keepsTCP(a *Scoped, p Placement) bool {
	if len(a.Replicas) == 0 {
		return false
	}
	if p.Spread != nil && *p.Spread {
		return false
	}
	if p.Replicas > 1 {
		return false
	}
	nodes := dedupe(p.Nodes)
	if len(nodes) == 0 {
		return true
	}
	return len(nodes) == 1 && nodes[0] == a.Replicas[0].NodeSlug
}

// tcpPortSpecs are the Engine's port bindings for an app's published ports.
func tcpPortSpecs(ports []TCPPort) []string {
	if len(ports) == 0 {
		return nil
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, strconv.Itoa(p.HostPort)+":"+strconv.Itoa(p.ContainerPort)+"/tcp")
	}
	return out
}
