package dockerx

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
)

// NOTE: the brief targets "github.com/docker/docker/api/types/nat", but the
// installed SDK (v25.0.6, see client.go) has no api/types/nat package —
// nat.PortSet/PortMap/ParsePortSpecs live in the separate
// "github.com/docker/go-connections/nat" module instead, which is what
// container.Config.ExposedPorts and container.HostConfig.PortBindings
// themselves import in this version.

type ContainerOpts struct {
	Name        string
	Image       string
	Labels      map[string]string
	Env         []string
	Cmd         []string
	Binds       []string
	Ports       []string
	Network     string
	HostNetwork bool
	ExtraHosts  []string
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	rc, err := c.api.ImagePull(ctx, ref, types.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer rc.Close()
	// Drain the pull progress stream; callers don't need the output.
	buf := make([]byte, 4096)
	for {
		if _, err := rc.Read(buf); err != nil {
			break
		}
	}
	return nil
}

func (c *Client) CreateContainer(ctx context.Context, opts ContainerOpts) (string, error) {
	var exposedPorts nat.PortSet
	var portBindings nat.PortMap
	var networkMode container.NetworkMode
	var networkingConfig *network.NetworkingConfig

	if opts.HostNetwork {
		networkMode = "host"
	} else {
		var err error
		exposedPorts, portBindings, err = nat.ParsePortSpecs(opts.Ports)
		if err != nil {
			return "", fmt.Errorf("parse port specs for %q: %w", opts.Name, err)
		}
		if opts.Network != "" {
			networkingConfig = &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					opts.Network: {},
				},
			}
		}
	}

	resp, err := c.api.ContainerCreate(ctx,
		&container.Config{
			Image:        opts.Image,
			Labels:       opts.Labels,
			Env:          opts.Env,
			Cmd:          opts.Cmd,
			ExposedPorts: exposedPorts,
		},
		&container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Binds:         opts.Binds,
			PortBindings:  portBindings,
			NetworkMode:   networkMode,
			ExtraHosts:    opts.ExtraHosts,
		},
		networkingConfig, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("create container %q: %w", opts.Name, err)
	}
	return resp.ID, nil
}

// EnsureNetwork creates the named Docker network if it doesn't already
// exist. It's idempotent: an "already exists" style error from the daemon
// is logged and swallowed rather than returned, since callers just want the
// network to be present afterward.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, types.NetworkCreate{}); err != nil {
		log.Printf("dockerx: create network %q: %v (assuming it already exists)", name, err)
	}
	return nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %q: %w", id, err)
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("stop container %q: %w", id, err)
	}
	return nil
}

func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove container %q: %w", id, err)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	rc, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return nil, fmt.Errorf("logs for container %q: %w", id, err)
	}
	return rc, nil
}

func (c *Client) IsRunning(ctx context.Context, id string) (bool, error) {
	info, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return false, fmt.Errorf("inspect container %q: %w", id, err)
	}
	return info.State != nil && info.State.Running, nil
}
