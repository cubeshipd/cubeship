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
	"github.com/docker/docker/api/types/image"
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
	Name   string
	Image  string
	Labels map[string]string
	Env    []string
	Cmd    []string
	// Entrypoint replaces the image's own. It exists for the one
	// container Cubeship starts from its own image to run something
	// other than the daemon — the dashboard's Next server — where Cmd
	// alone would be arguments to the daemon rather than a different
	// program.
	Entrypoint  []string
	Binds       []string
	Ports       []string
	Network     string
	HostNetwork bool
	ExtraHosts  []string
	// Privileged drops the container's isolation. Only BuildKit needs
	// it, and only because building an image means running one.
	Privileged bool
}

// RegistryAuth is a username and password for a registry Cubeship does
// not run. Cubeship's own registry is reached with a minted token
// instead — see authForRef — so a caller passes nil for it.
type RegistryAuth struct {
	Username string
	Password string
}

// PullImage fetches ref, authenticating with creds when they are given
// and with a minted token when the host is one the daemon signs for.
//
// Explicit credentials win. An operator who added a login for a host has
// said what to use there, and silently preferring anything else would
// make a failed pull impossible to explain.
func (c *Client) PullImage(ctx context.Context, ref string, creds *RegistryAuth) error {
	opts := image.PullOptions{}

	auth, ok, err := c.authFor(ref, creds)
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

// authForRef mints a fresh bearer token for the registry host of ref,
// if a signer is registered for it. It goes in RegistryToken, which the
// Engine sends to the registry as is; IdentityToken is not that — it is
// an OAuth refresh token the Engine takes to the token realm first,
// which for this registry is the public API through Traefik: a hairpin
// that needs the domain to resolve and the proxy to be up before the
// daemon can pull from a registry on its own loopback. The host is the segment before the
// first "/"; a reference with no "/" (e.g. "registry:2") is a Docker
// Hub official image and never carries credentials here. The token is
// scoped to exactly the repository being pulled (everything between the
// host and the last ":"), matching the least-privilege scope the
// signer's registry token endpoint would grant anyway.
// authFor picks the credentials for a pull: the ones given, or a minted
// token for a host the daemon signs for, or none at all.
func (c *Client) authFor(ref string, creds *RegistryAuth) (registry.AuthConfig, bool, error) {
	if creds != nil {
		return registry.AuthConfig{
			Username:      creds.Username,
			Password:      creds.Password,
			ServerAddress: registryHostOf(ref),
		}, true, nil
	}
	return c.authForRef(ref)
}

// registryHostOf is the address a reference's credentials are for. A
// reference whose first segment does not look like an address is a
// Docker Hub repository, and the Hub's own name is what the Engine
// expects there.
func registryHostOf(ref string) string {
	first, _, found := strings.Cut(ref, "/")
	if !found || (!strings.ContainsAny(first, ".:") && first != "localhost") {
		return "https://index.docker.io/v1/"
	}
	return first
}

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
	return registry.AuthConfig{RegistryToken: token, ServerAddress: host}, true, nil
}

// LoadImage imports an image tarball into the Engine's own store, which
// is how a build gets from BuildKit to something a container can run
// without a registry in between. On one VPS the image never has to leave
// the box.
func (c *Client) LoadImage(ctx context.Context, r io.Reader) error {
	resp, err := c.api.ImageLoad(ctx, r, true)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}
	defer resp.Body.Close()

	// Like a pull, a load reports its failures inside the progress
	// stream rather than as a transport error.
	if resp.JSON {
		dec := json.NewDecoder(resp.Body)
		for {
			var msg jsonmessage.JSONMessage
			if err := dec.Decode(&msg); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return fmt.Errorf("load image: %w", err)
			}
			if msg.Error != nil {
				return fmt.Errorf("load image: %s", msg.Error.Message)
			}
		}
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
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
			Entrypoint:   opts.Entrypoint,
			ExposedPorts: exposedPorts,
		},
		&container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Binds:         opts.Binds,
			PortBindings:  portBindings,
			NetworkMode:   networkMode,
			ExtraHosts:    opts.ExtraHosts,
			Privileged:    opts.Privileged,
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
	// Image is the reference the container was created from. The daemon
	// reads its own so it can start a sibling from the same image —
	// which is how the dashboard's container is the release rather than
	// a second thing to publish and keep in step.
	Image string
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
		// Config.Image is what was asked for — a tag — where the
		// container's own Image field is the digest it resolved to. A
		// tag is what a sibling should be started from: it is what the
		// operator upgrades.
		out.Image = info.Config.Image
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

// Exec runs a command inside a running container and returns everything
// it wrote, plus its exit status.
//
// The output is not demultiplexed into stdout and stderr: what calls
// this wants the transcript of a maintenance command, and a program that
// splits its progress across both streams is best read in the order it
// wrote them. `Tty: true` is what makes the Engine hand back one stream
// rather than the framed protocol.
func (c *Client) Exec(ctx context.Context, containerID string, cmd []string) (output string, exitCode int, err error) {
	created, err := c.api.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	})
	if err != nil {
		return "", 0, fmt.Errorf("create exec: %w", err)
	}

	attached, err := c.api.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return "", 0, fmt.Errorf("attach exec: %w", err)
	}
	defer attached.Close()

	// Read to EOF first: the command has not finished until its output
	// stream closes, and inspecting before then reports it still running.
	var buf strings.Builder
	if _, err := io.Copy(&buf, io.LimitReader(attached.Reader, maxExecOutput)); err != nil {
		return buf.String(), 0, fmt.Errorf("read exec output: %w", err)
	}

	inspected, err := c.api.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return buf.String(), 0, fmt.Errorf("inspect exec: %w", err)
	}
	return buf.String(), inspected.ExitCode, nil
}

// maxExecOutput caps what an exec can hand back. A garbage collection
// pass names every blob it walks, and on a busy registry that is more
// than anything reading it needs.
const maxExecOutput = 1 << 20

// ImageID resolves a reference to the id of the image it currently
// names, or "" when this host does not have it.
//
// The id rather than the reference, and that difference is the point.
// A tag is a moving name: `docker build -t cubeship/cubeship-frontend:local`
// makes a *new* image under the *same* tag, and anything comparing
// references sees no change. Comparing ids is what tells a rebuilt image
// from the one a container is already running.
//
// It doubles as the presence check, so nothing pulls what it already
// has — a round trip saved for the images from Docker Hub, and the
// difference between working and not for one built here, which exists
// in no registry to be pulled from.
func (c *Client) ImageID(ctx context.Context, ref string) (string, error) {
	info, _, err := c.api.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("inspect image %q: %w", ref, err)
	}
	return info.ID, nil
}

// Stats is one reading of what a container is using, in the terms the
// Engine reports them: counters, not rates.
//
// The counters rather than a percentage, because a percentage needs two
// readings and the Engine's own one-shot has nothing to compare against
// — see ContainerStats. Whoever samples these holds the previous
// reading and does the subtraction.
type Stats struct {
	// CPUTotal is nanoseconds of CPU this container has used since it
	// started, and CPUSystem the same for the whole host. The ratio of
	// their two deltas, times OnlineCPUs, is the percentage.
	CPUTotal  uint64
	CPUSystem uint64
	// OnlineCPUs is how many the host has, which is what makes 100%
	// mean "one core" rather than "the machine".
	OnlineCPUs int
	// MemoryBytes is what `docker stats` calls MEM USAGE: the cgroup's
	// usage minus its inactive page cache, which is memory the kernel
	// will reclaim rather than memory the process needs.
	MemoryBytes uint64
	// MemoryLimit is the cgroup's ceiling. On a container with no limit
	// set this is the host's total memory.
	MemoryLimit uint64
}

// ContainerStats reads one sample for a container.
//
// One-shot, which returns immediately with no previous reading in it.
// The alternative — the Engine's `stream=false` — computes a delta for
// you by sleeping about a second first, which is a second per container
// on every collection pass, and produces a percentage averaged over
// that second rather than over the interval anybody is charting.
func (c *Client) ContainerStats(ctx context.Context, containerID string) (Stats, error) {
	resp, err := c.api.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return Stats{}, fmt.Errorf("stats for container %q: %w", containerID, err)
	}
	defer resp.Body.Close()

	var raw container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Stats{}, fmt.Errorf("decode stats for container %q: %w", containerID, err)
	}

	cpus := int(raw.CPUStats.OnlineCPUs)
	if cpus == 0 {
		// Older Engines report the per-CPU slice instead, and a machine
		// with no answer at all is treated as one core rather than
		// zero — a division by it is the alternative.
		cpus = len(raw.CPUStats.CPUUsage.PercpuUsage)
	}
	if cpus == 0 {
		cpus = 1
	}

	// What the kernel counts as used includes the page cache it would
	// hand back under pressure. `docker stats` subtracts it and so does
	// this, or every container looks as though it is about to run out.
	// The key is named differently under cgroup v1 and v2.
	used := raw.MemoryStats.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if cached, ok := raw.MemoryStats.Stats[key]; ok {
			if cached < used {
				used -= cached
			}
			break
		}
	}

	return Stats{
		CPUTotal:    raw.CPUStats.CPUUsage.TotalUsage,
		CPUSystem:   raw.CPUStats.SystemUsage,
		OnlineCPUs:  cpus,
		MemoryBytes: used,
		MemoryLimit: raw.MemoryStats.Limit,
	}, nil
}
