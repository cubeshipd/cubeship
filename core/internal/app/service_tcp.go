package app

import (
	"context"
	"cubeship/internal/user"
)

// TCPPorts is an app's published ports.
func (s *Service) TCPPorts(ctx context.Context, caller *user.User, ref Reference) ([]TCPPort, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, err
	}
	return a.TCPPorts, nil
}

// AddTCPPort publishes a port of an app's container on a host port of the
// control plane. hostPort 0 picks one from TCPPortRangeStart. It is
// published from the app's next deploy, because a container's ports are
// fixed when it is created.
//
// There is no TLS in front of this and no proxy: whatever the app speaks
// on that port is on the open internet, the way an exposed database is.
// What makes it safe is the app's own authentication and the firewall,
// which is why it is an operator's deliberate act with its own endpoint.
func (s *Service) AddTCPPort(ctx context.Context, caller *user.User, ref Reference, containerPort, hostPort int) (*TCPPort, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if !validTCPPorts(containerPort, hostPort) {
		return nil, ErrInvalidTCPPort
	}
	for _, p := range a.TCPPorts {
		if p.ContainerPort == containerPort {
			return nil, ErrTCPPortExists
		}
	}
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	if !canPublishTCP(&a.App, here) {
		return nil, ErrTCPNeedsOneCopy
	}
	used, err := s.usedHostPorts(ctx)
	if err != nil {
		return nil, err
	}
	if hostPort == 0 {
		if hostPort, err = pickTCPPort(used); err != nil {
			return nil, err
		}
	} else if used[hostPort] {
		return nil, ErrTCPPortTaken
	}
	p, err := s.Repo().AddTCPPort(ctx, a.ID, containerPort, hostPort)
	if err != nil {
		return nil, err
	}
	s.syncPorts(ctx)
	return p, nil
}

// RemoveTCPPort stops publishing a port. The container running now keeps it
// until the next deploy.
func (s *Service) RemoveTCPPort(ctx context.Context, caller *user.User, ref Reference, portID int64) error {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return err
	}
	if err := s.Repo().RemoveTCPPort(ctx, a.ID, portID); err != nil {
		return err
	}
	s.syncPorts(ctx)
	return nil
}

// TCPPortTaken reports whether a host port is already published by
// anything on this instance, for a caller checking before it creates.
func (s *Service) TCPPortTaken(ctx context.Context, hostPort int) (bool, error) {
	used, err := s.usedHostPorts(ctx)
	if err != nil {
		return false, err
	}
	return used[hostPort], nil
}

// usedHostPorts is every host port an app, a datastore or a store holds.
func (s *Service) usedHostPorts(ctx context.Context) (map[int]bool, error) {
	used, err := s.Repo().UsedTCPPorts(ctx)
	if err != nil {
		return nil, err
	}
	if s.hostPorts != nil {
		others, err := s.hostPorts.ExposedPorts(ctx)
		if err != nil {
			return nil, err
		}
		for _, port := range others {
			used[port] = true
		}
	}
	return used, nil
}
