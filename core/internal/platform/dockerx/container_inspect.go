package dockerx

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
)

// restartPolicy: everything Cubeship runs is meant to come back after a
// reboot, except a one-shot, which has already done its job. Docker
// refuses a restart policy together with AutoRemove anyway.
func restartPolicy(opts ContainerOpts) container.RestartPolicy {
	if opts.AutoRemove {
		return container.RestartPolicy{}
	}
	return container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
}

func pidMode(opts ContainerOpts) container.PidMode {
	if opts.HostPID {
		return "host"
	}
	return ""
}

// EnsureNetwork creates the named Docker network if it doesn't already
// exist. It's idempotent: an "already exists" conflict from the daemon is
// treated as success, since callers just want the network to be present
// afterward. Any other failure is returned — swallowing those hides a
// genuinely broken Docker setup until it resurfaces as a much more
// confusing error at container-create time.
func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, network.CreateOptions{}); err != nil {
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
	Status    string
	Health    string
	StartedAt string
	Restarts  int
	TTY       bool
	ID        string
	Running   bool
	Labels    map[string]string
	// Image is the reference the container was created from. The daemon
	// reads its own so it can start a sibling from the same image —
	// which is how the dashboard's container is the release rather than
	// a second thing to publish and keep in step.
	Image string
}

// SpecOf reads back everything needed to create this container again.
//
// It exists for one caller and one reason: **replacing the daemon's own
// container with a newer image.** Nothing else here recreates a
// container it did not describe — `bootstrap.Ensure` writes the options
// and can therefore write them again — but the daemon's own were chosen
// by whoever installed it, and are the one set this process does not
// hold. Reading them back beats keeping a second copy that goes stale
// the first time somebody adds a flag.
//
// What is deliberately **not** carried over is the image: the caller
// supplies the new one, which is the whole point.
func (c *Client) SpecOf(ctx context.Context, name string) (ContainerOpts, error) {
	info, err := c.api.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return ContainerOpts{}, ErrContainerNotFound
		}
		return ContainerOpts{}, fmt.Errorf("inspect container %q: %w", name, err)
	}
	if info.ContainerJSONBase == nil || info.Config == nil || info.HostConfig == nil {
		return ContainerOpts{}, fmt.Errorf("inspect container %q: docker answered without its configuration", name)
	}

	opts := ContainerOpts{
		Name:        strings.TrimPrefix(info.Name, "/"),
		Image:       info.Config.Image,
		Labels:      info.Config.Labels,
		Env:         info.Config.Env,
		Cmd:         info.Config.Cmd,
		Entrypoint:  info.Config.Entrypoint,
		Binds:       info.HostConfig.Binds,
		ExtraHosts:  info.HostConfig.ExtraHosts,
		Privileged:  info.HostConfig.Privileged,
		CapDrop:     info.HostConfig.CapDrop,
		SecurityOpt: info.HostConfig.SecurityOpt,
		HostPID:     info.HostConfig.PidMode.IsHost(),
		Resources: Resources{
			NanoCPUs:    info.HostConfig.Resources.NanoCPUs,
			MemoryBytes: info.HostConfig.Resources.Memory,
		},
	}
	if info.HostConfig.NetworkMode.IsHost() {
		opts.HostNetwork = true
	} else if info.NetworkSettings != nil {
		// The first network is the one the container was created on and
		// the rest are ones it was connected to afterwards — a
		// distinction Docker does not keep, so they are sorted for a
		// stable answer and the first is taken as the primary. Every
		// container this recreates is on `cubeship` and nothing else.
		names := make([]string, 0, len(info.NetworkSettings.Networks))
		for name := range info.NetworkSettings.Networks {
			names = append(names, name)
		}
		sort.Strings(names)
		if len(names) > 0 {
			opts.Network, opts.AlsoNetworks = names[0], names[1:]
		}
	}
	for port, bindings := range info.HostConfig.PortBindings {
		for _, b := range bindings {
			spec := b.HostPort + ":" + port.Port() + "/" + port.Proto()
			if b.HostIP != "" {
				spec = b.HostIP + ":" + spec
			}
			opts.Ports = append(opts.Ports, spec)
		}
	}
	sort.Strings(opts.Ports)
	return opts, nil
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

	out := ContainerInfo{ID: info.ID, Running: info.State != nil && info.State.Running, Restarts: info.RestartCount}
	if info.State != nil {
		out.Status, out.StartedAt = info.State.Status, info.State.StartedAt
		if info.State.Health != nil {
			out.Health = info.State.Health.Status
		}
	}
	if info.Config != nil {
		out.TTY = info.Config.Tty
		out.Labels = info.Config.Labels
		// Config.Image is what was asked for — a tag — where the
		// container's own Image field is the digest it resolved to. A
		// tag is what a sibling should be started from: it is what the
		// operator upgrades.
		out.Image = info.Config.Image
	}
	return out, nil
}

// ContainerStartedAt is when a running container began this run. Docker
// uses the zero time for a container that has never started.
func (c *Client) ContainerStartedAt(ctx context.Context, id string) (*time.Time, error) {
	info, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("inspect container %q start time: %w", id, err)
	}
	if info.State == nil || !info.State.Running {
		return nil, nil
	}
	startedAt, err := time.Parse(time.RFC3339Nano, info.State.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse container %q start time: %w", id, err)
	}
	if startedAt.IsZero() {
		return nil, nil
	}
	return &startedAt, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %q: %w", id, err)
	}
	return nil
}

// SetResources changes a running container's ceiling in place.
//
// It is what makes raising an app's memory a request rather than a
// redeploy: the Engine writes the new numbers to the cgroup and the
// process inside never restarts.
//
// **Zero does not clear a limit.** The Engine merges an update rather
// than replacing the whole set, and reads a zero in any field as "leave
// that one alone" — so a container goes back to having no ceiling by
// being created again, not by being updated with nothing. Every caller
// here creates its containers often enough for that to be the next
// deploy or the next start.
func (c *Client) SetResources(ctx context.Context, id string, r Resources) error {
	if _, err := c.api.ContainerUpdate(ctx, id, container.UpdateConfig{Resources: resources(r)}); err != nil {
		return fmt.Errorf("set resources on container %q: %w", id, err)
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	return c.StopContainerWithOptions(ctx, id, 10*time.Second, "SIGTERM")
}

func (c *Client) StopContainerWithOptions(ctx context.Context, id string, grace time.Duration, signal string) error {
	seconds := int(grace / time.Second)
	if grace < 0 {
		seconds = -1
	}
	if err := c.api.ContainerStop(ctx, id, container.StopOptions{Timeout: &seconds, Signal: signal}); err != nil {
		return fmt.Errorf("stop container %q: %w", id, err)
	}
	return nil
}

// RemoveContainer deletes a container, forcing it if it is running.
//
// **A container that is not there satisfies the request.** Remove asks
// for it to be gone, and one somebody already removed by hand is gone —
// so not-found is success rather than an error every caller has to
// recognise and forgive. Returning it made "stop this app" fail
// permanently on an app whose container had been removed underneath the
// daemon, which is the one state where being told no is least useful.
func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
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
// A tag is a moving name: `docker build -t cubeship:local`
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
