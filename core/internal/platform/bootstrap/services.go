package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
)

// WriteRegistryConfig writes RegistryConfigYAML to the path
// RegistryContainerOpts' bind mount expects, and creates the registry's
// persistent storage directory. Call it before starting the registry
// container.
func WriteRegistryConfig(cfg *config.Config, apiHost, notifyURL, notifyToken string) error {
	// Docker would create a missing bind source itself, but only as
	// root-owned; creating it here keeps ownership with the daemon.
	if err := os.MkdirAll(cfg.DataDir+"/registry-data", 0o700); err != nil {
		return fmt.Errorf("create registry data dir: %w", err)
	}
	path := cfg.DataDir + "/registry-config.yml"
	if err := os.WriteFile(path, []byte(RegistryConfigYAML(apiHost, notifyURL, notifyToken)), 0o600); err != nil {
		return fmt.Errorf("write registry config: %w", err)
	}
	return nil
}

// WriteRegistryTokenCert writes certPEM (the self-signed certificate
// wrapping the daemon's registry-token signing key — see
// regauth.SelfSignedCert) to the path RegistryContainerOpts' bind mount
// expects. Call it before starting the registry container, and again
// any time the signing key changes (it never does today, but a future
// key-rotation feature would need this re-run).
func WriteRegistryTokenCert(cfg *config.Config, certPEM []byte) error {
	path := cfg.DataDir + "/registry-token.crt"
	if err := os.WriteFile(path, certPEM, 0o600); err != nil {
		return fmt.Errorf("write registry token cert: %w", err)
	}
	return nil
}

// The image builder. Everything Cubeship builds goes through it, and
// nothing else talks to it.
const (
	BuildKitContainerName = "cubeship-buildkit"
	BuildKitImage         = "moby/buildkit:v0.32.2"
)

// BuildKitSocket is where the daemon reaches buildkitd: a unix socket
// the container creates inside a host bind mount.
//
// A socket rather than a port because this is a build service running as
// root with no authentication of its own — anyone who can reach it can
// run anything. Filesystem permissions on a root-owned directory are the
// guard, and there is no port for a misconfigured firewall to expose.
func BuildKitSocket(cfg *config.Config) string {
	return "unix://" + cfg.DataDir + "/buildkit-run/buildkitd.sock"
}

// BuildKitContainerOpts describes the builder.
//
// Privileged, because building an image means running one: the build
// steps of a Dockerfile execute, and they need the same isolation
// primitives the Engine itself uses. This is the only container Cubeship
// runs that way.
//
// The layer cache is a host bind mount like everything else that has to
// survive — Ensure replaces a container whose configuration changed, and
// a cache inside the writable layer would be destroyed with it. Losing it
// is not incorrect, only slow, but slow builds are the thing a cache
// exists to prevent.
func BuildKitContainerOpts(cfg *config.Config) dockerx.ContainerOpts {
	// buildkitd creates its socket as root. The daemon in its container
	// is root too; on the host (`make dev`, CI) it is whoever ran it, and
	// a socket it cannot open is a builder that is never available.
	// buildkitd's --group hands the socket to that user's group instead.
	cmd := []string{"--oci-worker-gc-keepstorage=" + buildKitGCKeepStorage(diskTotal(cfg.DataDir+"/buildkit"))}
	if !cfg.InContainer {
		cmd = append(cmd, "--group", strconv.Itoa(os.Getgid()))
	}
	return dockerx.ContainerOpts{
		Name:       BuildKitContainerName,
		Image:      BuildKitImage,
		Cmd:        cmd,
		Privileged: true,
		Binds: []string{
			cfg.DataDir + "/buildkit:/var/lib/buildkit",
			cfg.DataDir + "/buildkit-run:/run/buildkit",
		},
		Network: dockerx.ManagementNetwork,
	}
}

// buildKitGCKeepStorage is the layer cache's size limit, in the
// "Reserved,Free,Maximum" megabytes buildkitd's own flag reads.
//
// buildkitd collects its cache by default, but against a ceiling of 80%
// of the disk (up to 100 GB): on a 150 GB box almost nothing was removed
// before the disk was nearly full, and every deploy added a build's worth
// of layers. The ceiling here is 20 GB, or a quarter of the disk when that
// is less, so the cache stays warm for the next few deploys and stops
// there. Collection keeps it under that ceiling, and when free
// space falls under 20% of the disk it may go down to half of it, never lower; that is buildkitd's own floor and
// which one explicit value would otherwise drop.
//
// A total of zero is a disk that could not be read: the fixed ceiling
// stays and the free-space floor is left out.
func buildKitGCKeepStorage(total int64) string {
	const mb = int64(1e6)
	maxUsed := 20 * 1000 * mb
	if total > 0 && total/4 < maxUsed {
		maxUsed = total / 4
	}
	var free int64
	if total > 0 {
		free = total / 5
	}
	return fmt.Sprintf("%d,%d,%d", maxUsed/2/mb, free/mb, maxUsed/mb)
}

// diskTotal is the size of the filesystem path is on, or zero when it
// cannot be read (a directory that does not exist yet, as in a test).
func diskTotal(path string) int64 {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Blocks) * int64(st.Bsize)
}

// The dashboard, which runs as a Next server in its own container.
//
// It used to be a static export compiled into the daemon, and that
// bought one binary at the cost of every route being static — no
// dynamic path segments, so what identified a resource travelled in the
// query string. Four levels deep that stopped being a constraint worth
// paying.
//
// It is its own image, published beside the daemon's at the same
// version. The daemon is *told* which one — CUBESHIP_WEB_IMAGE, baked
// into the daemon's image at the matching version and overridden by
// install.sh when it builds locally — rather than deriving it from its
// own image reference, because deriving means string surgery on a
// registry path that an operator is free to change.
const (
	FrontendContainerName = "cubeship-frontend"
	FrontendPort          = 3000
)

// FrontendAddress is where the daemon proxies a page request.
//
// The daemon is the only thing in front of this container — nothing
// publishes its port — so the address follows where the daemon runs,
// the same way every other address here does. On the host it is
// `make dashboard-dev`'s Next on :3001, which is deliberately not this
// container: a developer editing the dashboard wants hot reload, not a
// rebuilt image.
func FrontendAddress(cfg *config.Config) string {
	if cfg.InContainer {
		return fmt.Sprintf("%s:%d", FrontendContainerName, FrontendPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", localFrontendDevPort)
}

// localFrontendDevPort is where `make dashboard-dev` serves the dashboard.
const localFrontendDevPort = 3001

// FrontendContainerOpts describes the dashboard's container.
//
// Nothing publishes a port. The daemon reaches it by name on the shared
// network, and exposing the dashboard directly would be a second
// address answering for the same instance without the API beside it —
// where a session cookie set on one would not be sent to the other.
func FrontendContainerOpts(image string) dockerx.ContainerOpts {
	return dockerx.ContainerOpts{
		Name:    FrontendContainerName,
		Image:   image,
		Network: dockerx.ManagementNetwork,
	}
}

// EnsureFrontend starts the dashboard's container.
//
// A daemon on the host does not have one and does not need one:
// `make dashboard-dev` is what serves the dashboard there, with hot reload,
// which is what someone editing it wants.
func EnsureFrontend(ctx context.Context, docker dockerAPI, cfg *config.Config) error {
	if !cfg.InContainer {
		return nil
	}
	if cfg.WebImage == "" {
		return fmt.Errorf("nothing says which image the dashboard runs from; set CUBESHIP_WEB_IMAGE")
	}
	return Ensure(ctx, docker, FrontendContainerOpts(cfg.WebImage))
}

// EnsureBuildKit starts the builder if it is not already running.
//
// It is called before a build rather than at startup, and deliberately:
// an instance that only runs images it is given never builds anything,
// and a privileged container idling on it would be cost with no return.
// The first build on a box pays for the container starting; the ones
// after it do not.
func EnsureBuildKit(ctx context.Context, docker dockerAPI, cfg *config.Config) error {
	for _, dir := range []string{cfg.DataDir + "/buildkit", cfg.DataDir + "/buildkit-run"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create buildkit dir: %w", err)
		}
	}
	return Ensure(ctx, docker, BuildKitContainerOpts(cfg))
}

// TraefikImage is pinned, and the pin is not cosmetic.
//
// Traefik's Docker provider asks the Engine for a fixed API version, and
// up to v3.5 that version is 1.24. Docker Engine 28 raised the minimum
// it will answer to 1.40, so on any host with a current Engine those
// releases are refused — not degraded, refused: the provider retries
// forever, Traefik sees **no container at all**, and every router that
// comes from a container label silently does not exist.
//
// It is a nasty failure because nothing looks broken. Apps deploy,
// containers run, the file provider keeps working — so the daemon's own
// name still resolves and still gets a certificate — and the only
// symptom is that everything routed by label answers with Traefik's
// default self-signed certificate, which reads as a certificate problem
// rather than a proxy that cannot see anything.
//
// v3.6 negotiates the version. `DOCKER_API_VERSION` is not a way out:
// the older provider ignores it.
const TraefikImage = "traefik:v3.6"

// TraefikContainerOpts describes the proxy. With tls false there is no
// certificate resolver at all: nothing has a name to get a certificate
// for, so apps are served over plain HTTP. acmeEmail is the contact
// address Let's Encrypt registers, and may be empty — an account is
// opened without one.
//
// Setting a domain later changes these options, which is what makes
// Ensure replace the container — the resolver cannot be added to a
// running one.
//
// ownNames are the instance's own names — its domain and its registry's.
// Traefik answers to them as network aliases, so an app that calls the
// instance by its public name reaches the proxy on the bridge instead of
// leaving for the machine's public address, which a host that does not
// hairpin its own NAT never answers. Host and SNI are unchanged, so TLS
// verifies as it does from outside. Changing them replaces the container.
func TraefikContainerOpts(cfg *config.Config, tls bool, acmeEmail string, ownNames []string) dockerx.ContainerOpts {
	cmd := []string{
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		// No limit on reading a request. v3 bounds the whole request,
		// body included, at 60s, which cuts off any upload slower than a
		// minute. See networking.md.
		"--entrypoints.web.transport.respondingtimeouts.readtimeout=0",
		"--entrypoints.websecure.transport.respondingtimeouts.readtimeout=0",
		"--api.dashboard=false",
		"--accesslog=true",
		"--accesslog.format=json",
		"--accesslog.fields.defaultmode=drop",
		"--accesslog.fields.names.StartUTC=keep",
		"--accesslog.fields.names.RequestHost=keep",
		"--accesslog.fields.names.RequestMethod=keep",
		"--accesslog.fields.names.DownstreamStatus=keep",
		"--accesslog.fields.names.Duration=keep",
		"--accesslog.fields.names.RouterName=keep",
	}
	if tls {
		cmd = append(cmd,
			"--certificatesresolvers.letsencrypt.acme.tlschallenge=true",
			"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
			// Redirecting :80 is only right once :443 can actually serve.
			// Without a resolver it would send every visitor to a port
			// with no certificate.
			"--entrypoints.web.http.redirections.entryPoint.to=websecure",
			"--entrypoints.web.http.redirections.entryPoint.scheme=https",
			"--entrypoints.web.http.redirections.entryPoint.permanent=true")
		if acmeEmail != "" {
			cmd = append(cmd, "--certificatesresolvers.letsencrypt.acme.email="+acmeEmail)
		}
	}
	return dockerx.ContainerOpts{
		Name:  TraefikContainerName,
		Image: TraefikImage,
		Cmd:   cmd,
		Binds: []string{
			"/var/run/docker.sock:/var/run/docker.sock:ro",
			cfg.DataDir + "/letsencrypt:/letsencrypt",
			cfg.DataDir + "/traefik-dynamic:/etc/traefik/dynamic",
		},
		// The daemon may be on the host rather than on this network —
		// `make dev` runs it that way — and then the API router points
		// at the gateway rather than at a container name. Harmless when
		// the daemon is a container: the name simply goes unused.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		// On the shared network rather than the host's namespace, and
		// publishing the two ports it actually needs.
		//
		// It used to take the host's namespace for one reason: to reach
		// a daemon running on the host at 127.0.0.1. A daemon that is a
		// container is reached by name instead, and host networking
		// costs more than it buys — not least that it does not work at
		// all on Docker Desktop, where the Engine runs in a VM.
		Network:      dockerx.ManagementNetwork,
		AlsoNetworks: []string{Network},
		Aliases:      ownNames,
		Ports:        []string{"80:80", "443:443"},
	}
}

// APIRouterConfigYAML returns a Traefik file-provider dynamic
// configuration routing apiHost to the daemon's own API.
//
// The file provider rather than the Docker one, even now that the daemon
// is a container: Traefik discovers containers by their labels, and a
// container cannot label itself after it has started. The daemon writes
// this instead, which also means the route exists before anything has to
// go looking for it.
//
// daemonAddress is where Traefik reaches the daemon — its container name
// on the shared network, or the host gateway when the daemon runs on the
// host.
func APIRouterConfigYAML(apiHost, daemonAddress string) string {
	return fmt.Sprintf(`http:
  routers:
    cubeship-api:
      rule: "Host(`+"`%s`"+`)"
      entrypoints:
        - websecure
      tls:
        certResolver: letsencrypt
      service: cubeship-api
  services:
    cubeship-api:
      loadBalancer:
        servers:
          - url: "http://%s"
`, apiHost, daemonAddress)
}

// EnsureTraefikDirs creates the two directories Traefik is given.
//
// It exists for the machines in a cluster, which run the same Traefik
// and write nothing into either: the control plane's are made on the
// way past by whatever writes into them, and a worker has nothing to
// write. A file provider pointed at a directory that is not there
// complains on every reload for the life of the container.
func EnsureTraefikDirs(cfg *config.Config) error {
	for _, dir := range []string{cfg.DataDir + "/letsencrypt", cfg.DataDir + "/traefik-dynamic"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}
