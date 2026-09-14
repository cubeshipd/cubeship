package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"cubeship/internal/platform/dockerx"
	"github.com/docker/go-connections/nat"
)

type networkDocker interface {
	EnsureNetwork(context.Context, string) error
	ReconcileNetworks(context.Context, string, []string, []string) error
}

// DaemonReplacement applies install-time isolation to the built-in updater,
// which otherwise carries the previous release's public bindings forward.
func DaemonReplacement(old dockerx.ContainerOpts) (dockerx.ContainerOpts, error) {
	if old.HostNetwork {
		return old, fmt.Errorf("a daemon using host networking needs reinstalling on the management bridge")
	}
	_, bindings, err := nat.ParsePortSpecs(old.Ports)
	if err != nil {
		return old, err
	}
	next := old
	next.Network = dockerx.ManagementNetwork
	next.AlsoNetworks = nil
	for _, name := range append([]string{old.Network}, old.AlsoNetworks...) {
		if name != "" && name != Network && name != dockerx.ManagementNetwork && !slices.Contains(next.AlsoNetworks, name) {
			next.AlsoNetworks = append(next.AlsoNetworks, name)
		}
	}
	next.Ports = nil
	for port, hosts := range bindings {
		for _, host := range hosts {
			binding := "127.0.0.1:" + host.HostPort + ":" + port.Port() + "/" + port.Proto()
			if !slices.Contains(next.Ports, binding) {
				next.Ports = append(next.Ports, binding)
			}
		}
	}
	sort.Strings(next.Ports)
	return next, nil
}

// PrepareNetworks also moves containers from older releases, including
// an idle builder that would otherwise remain exposed until its next build.
func PrepareNetworks(ctx context.Context, docker networkDocker, inContainer bool) error {
	for _, name := range []string{Network, dockerx.ManagementNetwork} {
		if err := docker.EnsureNetwork(ctx, name); err != nil {
			return fmt.Errorf("prepare %s: %w", name, err)
		}
	}
	if inContainer {
		if err := docker.ReconcileNetworks(ctx, DaemonContainerName, []string{dockerx.ManagementNetwork}, []string{Network}); err != nil {
			return fmt.Errorf("isolate daemon: %w", err)
		}
	}
	for _, name := range []string{PostgresContainerName, RegistryContainerName, FrontendContainerName, BuildKitContainerName, TraefikContainerName} {
		remove := []string{Network}
		if name == TraefikContainerName {
			remove = nil
		}
		if err := docker.ReconcileNetworks(ctx, name, []string{dockerx.ManagementNetwork}, remove); err != nil && !errors.Is(err, dockerx.ErrContainerNotFound) {
			return fmt.Errorf("isolate %s: %w", name, err)
		}
	}
	return nil
}
