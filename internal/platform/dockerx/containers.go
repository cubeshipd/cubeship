package dockerx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
)

// ErrContainerNotFound is returned by InspectContainerByName when no
// container with the requested name or ID exists.
var ErrContainerNotFound = errors.New("container not found")

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
	opts := types.ImagePullOptions{}
	auth, ok, err := c.authForRef(ref)
	if err != nil {
		return fmt.Errorf("sign registry auth for %q: %w", ref, err)
	}
	if ok {
		encoded, err := registry.EncodeAuthConfig(auth)
		if err != nil {
			return fmt.Errorf("encode registry auth for %q: %w", ref, err)
		}
		opts.RegistryAuth = encoded
	}

	rc, err := c.api.ImagePull(ctx, ref, opts)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer rc.Close()

	// The Engine API reports most pull failures (auth, TLS, "manifest
	// unknown") as HTTP 200 with a JSON error object *inside* the
	// progress stream rather than as a transport error, so draining the
	// body blindly reports a failed pull as a success. Decode every
	// message and surface the first error one.
	dec := json.NewDecoder(rc)
	for {
		var msg jsonmessage.JSONMessage
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("pull image %q: read progress stream: %w", ref, err)
		}
		if msg.Error != nil {
			return fmt.Errorf("pull image %q: %s", ref, msg.Error.Message)
		}
		if msg.ErrorMessage != "" {
			return fmt.Errorf("pull image %q: %s", ref, msg.ErrorMessage)
		}
	}
	return nil
}

// authForRef mints a fresh identity token for the registry host of ref,
// if a signer is registered for it. The host is the segment before the
// first "/"; a reference with no "/" (e.g. "registry:2") is a Docker
// Hub official image and never carries credentials here. The token is
// scoped to exactly the repository being pulled (everything between the
// host and the last ":"), matching the least-privilege scope the
// signer's registry token endpoint would grant anyway.
func (c *Client) authForRef(ref string) (registry.AuthConfig, bool, error) {
	if len(c.registryTokenSigners) == 0 {
		return registry.AuthConfig{}, false, nil
	}
	i := strings.Index(ref, "/")
	if i < 0 {
		return registry.AuthConfig{}, false, nil
	}
	host := ref[:i]
	signer, ok := c.registryTokenSigners[host]
	if !ok {
		return registry.AuthConfig{}, false, nil
	}

	repo := ref[i+1:]
	if j := strings.LastIndex(repo, ":"); j >= 0 {
		repo = repo[:j]
	}

	token, err := signer(repo)
	if err != nil {
		return registry.AuthConfig{}, false, err
	}
	return registry.AuthConfig{IdentityToken: token, ServerAddress: host}, true, nil
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
// exist. It's idempotent: an "already exists" conflict from the daemon is
// treated as success, since callers just want the network to be present
// afterward. Any other failure is returned — swallowing those hides a
// genuinely broken Docker setup until it resurfaces as a much more
// confusing error at container-create time.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, types.NetworkCreate{}); err != nil {
		if isAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create network %q: %w", name, err)
	}
	return nil
}

// isAlreadyExists reports whether err is the daemon's "this object
// already exists" conflict. The installed SDK (v25.0.6) wraps a 409 as an
// errdefs.ErrConflict, but the daemon also returns plain 500s carrying an
// "already exists" message for some object types depending on API version,
// so the message is checked as a fallback.
func isAlreadyExists(err error) bool {
	if errdefs.IsConflict(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "already in use")
}

// ContainerInfo is what an inspection tells us about an existing
// container. Labels matter as much as the rest: they are where the
// configuration a container was created from is recorded, which is how a
// caller tells a container that is merely running from one that is
// running the right thing.
type ContainerInfo struct {
	ID      string
	Running bool
	Labels  map[string]string
}

// InspectContainerByName looks up a container by name (or ID). It returns
// ErrContainerNotFound if no such container exists, so callers can
// distinguish "not there yet" from "Docker is broken".
func (c *Client) InspectContainerByName(ctx context.Context, name string) (ContainerInfo, error) {
	info, err := c.api.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return ContainerInfo{}, ErrContainerNotFound
		}
		return ContainerInfo{}, fmt.Errorf("inspect container %q: %w", name, err)
	}
	if info.ContainerJSONBase == nil {
		return ContainerInfo{}, fmt.Errorf("inspect container %q: empty response from docker", name)
	}

	out := ContainerInfo{ID: info.ID, Running: info.State != nil && info.State.Running}
	if info.Config != nil {
		out.Labels = info.Config.Labels
	}
	return out, nil
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

// Logs returns id's stdout/stderr, demand-multiplexed per Docker's log
// framing (see stdcopy in callers). tail limits it to that many trailing
// lines; an empty tail returns the whole log.
func (c *Client) Logs(ctx context.Context, id, tail string) (io.ReadCloser, error) {
	rc, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: tail})
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
