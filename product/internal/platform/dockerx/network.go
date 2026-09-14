package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
)

const (
	ApplicationNetwork = "cubeship"
	ManagementNetwork  = "cubeship-management"
)

// ReconcileNetworks attaches new networks before removing old ones, so a
// failed attachment cannot strand a daemon migrating its own connection.
func (c *Client) ReconcileNetworks(ctx context.Context, id string, add, remove []string) error {
	info, err := c.api.ContainerInspect(ctx, id)
	if errdefs.IsNotFound(err) {
		return ErrContainerNotFound
	}
	if err != nil {
		return err
	}
	if info.NetworkSettings == nil {
		return fmt.Errorf("inspect %s: network settings missing", id)
	}
	for _, name := range add {
		if _, exists := info.NetworkSettings.Networks[name]; exists {
			continue
		}
		if err := c.api.NetworkConnect(ctx, name, id, &network.EndpointSettings{}); err != nil {
			return fmt.Errorf("connect %s to %s: %w", id, name, err)
		}
	}
	for _, name := range remove {
		if _, exists := info.NetworkSettings.Networks[name]; !exists {
			continue
		}
		if err := c.api.NetworkDisconnect(ctx, name, id, false); err != nil {
			return fmt.Errorf("disconnect %s from %s: %w", id, name, err)
		}
	}
	return nil
}
