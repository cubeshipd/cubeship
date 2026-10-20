package dockerx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
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
	Entrypoint []string
	Binds      []string
	Ports      []string
	Network    string
	// AlsoNetworks are networks the container joins besides Network.
	//
	// It exists for the cluster's overlay: a container that has to be
	// reachable from another machine is on the local bridge *and* on
	// the mesh, and the bridge is what everything on this box already
	// resolves it by. Connected after the container is created and
	// **before it is started**, so its process has the interface from
	// the first instruction it runs — a server that binds or registers
	// at startup would otherwise come up without it.
	//
	// Not passed to ContainerCreate as a second endpoint: an Engine
	// older than 25 refuses more than one there, and connecting after
	// is what every version does the same way.
	AlsoNetworks []string
	// Aliases are extra names this container answers to on every
	// network it joins — Network and each of AlsoNetworks.
	//
	// It exists because a container's *name* is not always an address.
	// An app's carries the deployment that created it, so that a
	// machine can answer "am I already running this" from the name
	// alone — and that makes it a name which changes under anything
	// that wrote it down. An alias is the part that does not change,
	// and Docker's embedded DNS answers it with **every** container
	// that holds it, so the copies of a scaled-out app are balanced
	// over without anything here doing the balancing.
	Aliases     []string
	HostNetwork bool
	ExtraHosts  []string
	// Privileged drops the container's isolation. Only BuildKit needs
	// it, and only because building an image means running one.
	Privileged  bool
	CapDrop     []string
	SecurityOpt []string
	// HostPID puts the container in the host's PID namespace, which is
	// what makes PID 1 there addressable — the one thing a container
	// needs to step out into the host's namespaces with nsenter. See
	// internal/platform/hostexec, the only caller.
	HostPID bool
	// AutoRemove asks the Engine to delete the container when it exits.
	// For a one-shot that is read through ContainerWait, not through
	// its logs.
	//
	// It was declared here and never passed to the Engine, which went
	// unnoticed because the one caller that used it — RunOneShot —
	// removes the container itself anyway. The one that relied on it
	// was the updater, and a leftover updater is a **name already in
	// use**: the second update an instance ever ran failed on it, with
	// the dashboard already replaced and the daemon not.
	AutoRemove bool
	// ShmSize is /dev/shm in bytes, zero for the Engine's 64 MiB.
	//
	// It exists for Postgres, which takes its dynamic shared memory from
	// /dev/shm: a parallel query bigger than what is there fails with
	// "could not resize shared memory segment", on a database that is
	// otherwise fine. Compose files for Postgres set it for this reason
	// and nothing else does.
	ShmSize int64
	// Resources is the ceiling this container runs under.
	Resources Resources
	// TTY gives the container a terminal and an open stdin that closes
	// when whoever attached goes — the shape of an interactive session,
	// and of nothing else here. ConsoleSize is its first size, rows then
	// columns, as the Engine takes it.
	TTY         bool
	ConsoleSize [2]uint
}

// Resources is how much of the machine a container may take: CPU quota
// and a memory ceiling. Zero in either is no limit, which is what every
// container Cubeship ran before this existed.
//
// It is the **only** part of a container's configuration the Engine can
// change on a running container. Everything else here — image, binds,
// ports, environment — is fixed when the container is created, which is
// why a new setting anywhere else means a new container and a new
// ceiling does not.
type Resources struct {
	// NanoCPUs is CPU quota in billionths of a core: 1_500_000_000 is a
	// core and a half. Docker's own --cpus, which is a ceiling rather
	// than a share — a container at its limit is throttled, not merely
	// preferred less when the machine is busy.
	NanoCPUs int64
	// MemoryBytes is a hard ceiling the kernel enforces by killing the
	// process that crosses it. Lowering one below what a container is
	// already holding therefore kills it on the spot, which is the one
	// thing about this setting that surprises people.
	MemoryBytes int64
}

// Unlimited reports whether nothing is capped.
func (r Resources) Unlimited() bool { return r.NanoCPUs == 0 && r.MemoryBytes == 0 }

// resources turns a ceiling into what the Engine wants.
//
// MemorySwap is pinned to the memory limit rather than left alone,
// because Docker's own default is twice it: a container told to hold
// 512 MiB would be allowed a gigabyte of it the moment the host has
// swap, and the same number would mean two different things on two
// machines.
func resources(r Resources) container.Resources {
	out := container.Resources{NanoCPUs: r.NanoCPUs, Memory: r.MemoryBytes}
	if r.MemoryBytes > 0 {
		out.MemorySwap = r.MemoryBytes
	}
	return out
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
//
// A transient transport failure — connection reset mid-layer, EOF,
// timeout — is tried again a few times with backoff. Auth and "manifest
// unknown" are not; those will not change on another attempt. Every
// deploy path (apps, datastores, updates) goes through here, so the
// retry lives once rather than at each caller.
func (c *Client) PullImage(ctx context.Context, ref string, creds *RegistryAuth) error {
	var last error
	for attempt := 0; attempt < pullAttempts; attempt++ {
		if attempt > 0 {
			wait := pullRetryBackoff[attempt-1]
			if err := sleepCtx(ctx, wait); err != nil {
				return err
			}
		}
		err := c.pullOnce(ctx, ref, creds)
		if err == nil {
			return nil
		}
		last = err
		if attempt == pullAttempts-1 || !isTransientPullError(err) {
			return err
		}
	}
	return last
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) pullOnce(ctx context.Context, ref string, creds *RegistryAuth) error {
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
	resp, err := c.api.ImageLoad(ctx, r, client.ImageLoadWithQuiet(true))
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
					opts.Network: {Aliases: opts.Aliases},
				},
			}
		}
	}

	// One name for what goes wrong below, because a container that is
	// created and then not fully wired has to be removed rather than
	// left half-made.
	resp, err := c.api.ContainerCreate(ctx,
		&container.Config{
			Image:        opts.Image,
			Labels:       opts.Labels,
			Env:          opts.Env,
			Cmd:          opts.Cmd,
			Entrypoint:   opts.Entrypoint,
			ExposedPorts: exposedPorts,
			Tty:          opts.TTY,
			OpenStdin:    opts.TTY,
			StdinOnce:    opts.TTY,
			AttachStdin:  opts.TTY,
			AttachStdout: opts.TTY,
			AttachStderr: opts.TTY,
		},
		&container.HostConfig{
			RestartPolicy: restartPolicy(opts),
			Binds:         opts.Binds,
			PortBindings:  portBindings,
			NetworkMode:   networkMode,
			ExtraHosts:    opts.ExtraHosts,
			Privileged:    opts.Privileged,
			CapDrop:       opts.CapDrop,
			SecurityOpt:   opts.SecurityOpt,
			PidMode:       pidMode(opts),
			ShmSize:       opts.ShmSize,
			Resources:     resources(opts.Resources),
			AutoRemove:    opts.AutoRemove,
			ConsoleSize:   opts.ConsoleSize,
		},
		networkingConfig, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("create container %q: %w", opts.Name, err)
	}

	for _, name := range opts.AlsoNetworks {
		if name == "" || name == opts.Network {
			continue
		}
		// The same aliases on every network it joins. A name that
		// resolves on the bridge and not on the mesh is one that works
		// until the app it addresses moves machine, which is the worst
		// of the three possible answers.
		var settings *network.EndpointSettings
		if len(opts.Aliases) > 0 {
			settings = &network.EndpointSettings{Aliases: opts.Aliases}
		}
		if err := c.api.NetworkConnect(ctx, name, resp.ID, settings); err != nil {
			// A container on fewer networks than it was asked for is
			// not the container that was asked for: it would come up,
			// pass its health check, and be unreachable from half the
			// cluster. Removed here rather than returned running.
			c.RemoveContainer(ctx, resp.ID)
			return "", fmt.Errorf("connect container %q to network %q: %w", opts.Name, name, err)
		}
	}
	return resp.ID, nil
}
