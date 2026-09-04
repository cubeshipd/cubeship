package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/platform/traefik"

	"github.com/docker/docker/errdefs"
)

const registryPort = 5000

// RegistryContainerName is the embedded registry, and the name the
// daemon pulls from when both are containers.
const RegistryContainerName = "cubeship-registry"

// Network is the bridge every Cubeship container shares, the daemon's
// own included when it runs as one. Container DNS on it is what lets
// each part address the others by name.
const Network = "cubeship"

// DaemonContainerName is what the daemon is called when it runs as a
// container, and so the name everything else reaches it by.
const DaemonContainerName = "cubeship-daemon"

// The daemon's own Postgres, when it isn't pointed at an external one
// with CUBESHIP_DATABASE_URL.
//
// It is also published on loopback, which is what a daemon running on
// the host connects to. Nothing outside this host can reach either way
// in.
const (
	PostgresContainerName = "cubeship-postgres"
	PostgresImage         = "postgres:16-alpine"
	PostgresPort          = 5432
	PostgresUser          = "cubeship"
	PostgresDatabase      = "cubeship"
)

// PostgresDSN is the connection string for the managed Postgres.
//
// Which address it is reached at depends on where the daemon runs: a
// container talks to it by name over the shared network, a host process
// over the loopback publication.
//
// sslmode=disable is correct here and only here: the connection never
// leaves this host — one interface or one bridge, never a wire.
func PostgresDSN(cfg *config.Config, password string) string {
	host := "127.0.0.1"
	if cfg.InContainer {
		host = PostgresContainerName
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		PostgresUser, url.QueryEscape(password), host, PostgresPort, PostgresDatabase)
}

// DaemonAddress is where the daemon is reached from another container —
// the registry posting a webhook, Traefik routing the API.
func DaemonAddress(cfg *config.Config, port int) string {
	if cfg.InContainer {
		return fmt.Sprintf("%s:%d", DaemonContainerName, port)
	}
	// A host process is not on the bridge, so a container reaches it
	// through the gateway rather than by name (see ExtraHosts on the
	// containers that need it).
	return fmt.Sprintf("host.docker.internal:%d", port)
}

// LocalRegistryAddress is where the daemon pulls an app's image from.
// Never the public name: that would hairpin out to this host's own
// address and need a certificate to already exist, which must not be
// what a deploy waits on.
func LocalRegistryAddress(cfg *config.Config) string {
	if cfg.InContainer {
		return fmt.Sprintf("%s:%d", RegistryContainerName, registryPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", registryPort)
}

// PostgresContainerOpts describes the daemon's own database container.
//
// The data directory is a bind mount on the host, so recreating the
// container — a version bump, a config change — keeps every row. Losing
// it would mean losing every app, user and API key on the instance.
func PostgresContainerOpts(cfg *config.Config, password string) dockerx.ContainerOpts {
	return dockerx.ContainerOpts{
		Name:  PostgresContainerName,
		Image: PostgresImage,
		Env: []string{
			"POSTGRES_USER=" + PostgresUser,
			"POSTGRES_PASSWORD=" + password,
			"POSTGRES_DB=" + PostgresDatabase,
		},
		Ports:   []string{fmt.Sprintf("127.0.0.1:%d:5432", PostgresPort)},
		Binds:   []string{cfg.DataDir + "/postgres:/var/lib/postgresql/data"},
		Network: "cubeship",
	}
}

// EnsurePostgresDataDir creates the bind-mount source before the
// container starts. Docker would create it too, but root-owned; creating
// it here keeps ownership with the daemon.
func EnsurePostgresDataDir(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.DataDir+"/postgres", 0o700); err != nil {
		return fmt.Errorf("create postgres data dir: %w", err)
	}
	return nil
}

// RegistryContainerOpts describes the embedded registry. registryHost
// is the public name it is reached at; it only exists once the instance
// has a domain, which is why the daemon does not start this container
// before then.
func RegistryContainerOpts(cfg *config.Config, registryHost string, tls bool) dockerx.ContainerOpts {
	labels := traefik.Labels("registry", registryHost, registryPort, tls)
	return dockerx.ContainerOpts{
		Name:    RegistryContainerName,
		Image:   "registry:2",
		Labels:  labels,
		Network: "cubeship",
		// Also published on localhost, plain HTTP, bypassing Traefik/TLS.
		// Docker trusts 127.0.0.0/8 as insecure-by-default, so this needs
		// no daemon.json changes — useful for local pushes and is how
		// Task 20's integration test pushes without needing a real
		// public domain for ACME.
		Ports: []string{"127.0.0.1:5000:5000"},
		// The registry container lives on the "cubeship" bridge network,
		// not the host's network namespace, so "127.0.0.1" inside it is
		// the container's own loopback, not the daemon's. host.docker.internal
		// (with the "host-gateway" magic value, needed on Linux — Docker
		// Desktop already provides it) resolves to the host, which is what
		// the config.yml notification endpoint (written by
		// WriteRegistryConfig) must point through to actually reach
		// cubeshipd.
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		// registry:2's baked-in default config.yml has no `notifications:`
		// key, and the REGISTRY_NOTIFICATIONS_ENDPOINTS_0_* env-var
		// overlay only patches keys that already exist in the base
		// config — so those env vars are silently ignored and the
		// registry never calls the webhook. Mounting a full replacement
		// config.yml (written by WriteRegistryConfig) is the only way to
		// actually configure notifications on this image.
		Binds: []string{
			cfg.DataDir + "/registry-config.yml:/etc/docker/registry/config.yml:ro",
			// The certificate the config.yml auth section's
			// rootcertbundle points at, used to verify tokens the daemon
			// signs. Written by WriteRegistryTokenCert before the
			// container starts — if it doesn't exist, Docker would
			// create a *directory* here and the registry would refuse to
			// start.
			cfg.DataDir + "/registry-token.crt:/etc/docker/registry/token.crt:ro",
			// Without this, pushed images live only in the container's
			// writable layer: recreating the registry container (a host
			// reboot, a config change) destroys every image ever pushed,
			// including whatever is currently deployed.
			cfg.DataDir + "/registry-data:/var/lib/registry",
		},
	}
}

// RegistryConfigYAML returns the registry:2 config.yml this daemon
// needs: the image's own default settings (storage, http, health), a
// token auth section pointing at the daemon's own /v2/token endpoint,
// plus a notifications.endpoint pointing at the daemon's webhook. This
// replaces (not merges with) the image's baked-in config, so it must
// carry everything the registry needs to run, not just the
// notifications section.
//
// The auth section matters: the registry is published both on
// 127.0.0.1:5000 and, through Traefik, at registry.<domain> over TLS.
// Without it, anyone on the internet could push an image the daemon
// would then pull and run on the VPS. Token auth (rather than a shared
// htpasswd credential) is what lets each user push only to their own
// org's namespace — see internal/api's registry token handler and
// internal/regauth for the signing side.
//
// notifyToken is the daemon's system token (cfg.Token). It is the
// shared secret on the notification endpoint's Authorization header, so
// the deliberately-unauthenticated /hooks/registry route can still tell
// a genuine push notification from a forged one — it has nothing to do
// with registry push/pull authentication, which is entirely the token
// realm's job now.
func RegistryConfigYAML(apiHost, notifyURL, notifyToken string) string {
	realm := "https://" + apiHost + "/v2/token"
	return fmt.Sprintf(`version: 0.1
log:
  fields:
    service: registry
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /var/lib/registry
auth:
  token:
    realm: %s
    service: %s
    issuer: %s
    rootcertbundle: /etc/docker/registry/token.crt
http:
  addr: :5000
  headers:
    X-Content-Type-Options: [nosniff]
health:
  storagedriver:
    enabled: true
    interval: 10s
    threshold: 3
notifications:
  endpoints:
    - name: cubeshipd
      url: %s
      headers:
        Authorization: [Bearer %s]
      timeout: 5s
      threshold: 5
      backoff: 1s
`, realm, regauth.TokenService, regauth.TokenIssuer, notifyURL, notifyToken)
}

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
	return dockerx.ContainerOpts{
		Name:       BuildKitContainerName,
		Image:      BuildKitImage,
		Privileged: true,
		Binds: []string{
			cfg.DataDir + "/buildkit:/var/lib/buildkit",
			cfg.DataDir + "/buildkit-run:/run/buildkit",
		},
		Network: "cubeship",
	}
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

// TraefikContainerOpts describes the proxy. An empty acmeEmail means no
// certificate resolver at all: Let's Encrypt will not register an account
// without a contact address, so until one is configured apps are served
// over plain HTTP.
//
// Adding the email later changes these options, which is what makes
// Ensure replace the container — the resolver cannot be added to a
// running one.
func TraefikContainerOpts(cfg *config.Config, acmeEmail string) dockerx.ContainerOpts {
	cmd := []string{
		"--providers.docker=true",
		"--providers.docker.exposedbydefault=false",
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--api.dashboard=false",
	}
	if acmeEmail != "" {
		cmd = append(cmd,
			"--certificatesresolvers.letsencrypt.acme.tlschallenge=true",
			"--certificatesresolvers.letsencrypt.acme.email="+acmeEmail,
			"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
			// Redirecting :80 is only right once :443 can actually serve.
			// Without a resolver it would send every visitor to a port
			// with no certificate.
			"--entrypoints.web.http.redirections.entryPoint.to=websecure",
			"--entrypoints.web.http.redirections.entryPoint.scheme=https",
			"--entrypoints.web.http.redirections.entryPoint.permanent=true",
		)
	}
	return dockerx.ContainerOpts{
		Name:  "cubeship-traefik",
		Image: "traefik:v3.1",
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
		Network: Network,
		Ports:   []string{"80:80", "443:443"},
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

// WriteAPIRouterConfig writes APIRouterConfigYAML to the path Traefik's
// file provider watches (see the Binds entry above). Call it before
// starting Traefik, and again any time cfg changes.
func WriteAPIRouterConfig(cfg *config.Config, apiHost string, daemonPort int) error {
	daemonAddress := DaemonAddress(cfg, daemonPort)
	dir := cfg.DataDir + "/traefik-dynamic"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create traefik dynamic config dir: %w", err)
	}
	path := dir + "/api.yml"
	if err := os.WriteFile(path, []byte(APIRouterConfigYAML(apiHost, daemonAddress)), 0o600); err != nil {
		return fmt.Errorf("write traefik dynamic config: %w", err)
	}
	return nil
}

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string, creds *dockerx.RegistryAuth) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	InspectContainerByName(ctx context.Context, name string) (dockerx.ContainerInfo, error)
}

// ConfigHashLabel records, on the container itself, a fingerprint of the
// options it was created from. Comparing it against what the current
// binary wants is what lets Ensure tell "this container is running" from
// "this container is running the right thing".
const ConfigHashLabel = "cubeship.config-hash"

// configHash fingerprints the options a container should be created
// from. Anything that would need a new container to take effect — the
// image, the binds, the environment, the ports — is part of it.
//
// JSON is the serialization because encoding/json orders map keys, so
// the same options always hash the same way. The hash is not a security
// boundary, just a change detector.
func configHash(opts dockerx.ContainerOpts) string {
	// The label itself is excluded: it holds the result.
	opts.Labels = withoutConfigHash(opts.Labels)

	encoded, err := json.Marshal(opts)
	if err != nil {
		// ContainerOpts is plain data and cannot fail to encode. If that
		// ever changes, a hash nothing matches is the safe answer: it
		// recreates rather than skipping a change.
		return "unhashable"
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func withoutConfigHash(labels map[string]string) map[string]string {
	if _, present := labels[ConfigHashLabel]; !present {
		return labels
	}
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		if k != ConfigHashLabel {
			out[k] = v
		}
	}
	return out
}

// withConfigHash returns opts carrying its own fingerprint as a label.
func withConfigHash(opts dockerx.ContainerOpts) dockerx.ContainerOpts {
	hash := configHash(opts)

	labels := make(map[string]string, len(opts.Labels)+1)
	for k, v := range opts.Labels {
		labels[k] = v
	}
	labels[ConfigHashLabel] = hash
	opts.Labels = labels
	return opts
}

// Ensure makes the described infrastructure container exist, run, and
// match the configuration this binary wants.
//
// A container whose configuration still matches is left alone if it is
// running, and started if it is stopped — a host reboot otherwise leaves
// the daemon "up" with no proxy running at all. One whose configuration
// has changed is replaced, because Docker cannot alter the image, binds,
// ports or environment of an existing container: the only way a new
// setting takes effect is a new container.
//
// Replacing is safe for all three of these because everything they must
// keep lives in a host bind mount — the registry's images, Traefik's
// acme.json, Postgres' data directory. It does mean a few seconds of
// downtime for that container on a release that changes its
// configuration, which is the cost of the alternative being silently
// running stale settings.
//
// Creation failures are returned rather than assumed to mean "already
// exists" — a bad bind path or port conflict is a real failure.
func Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error {
	opts = withConfigHash(opts)
	want := opts.Labels[ConfigHashLabel]

	existing, err := docker.InspectContainerByName(ctx, opts.Name)
	switch {
	case err == nil && existing.Labels[ConfigHashLabel] != want:
		// Also covers a container created before this label existed,
		// which is exactly the case operators used to fix by hand with
		// `docker rm -f`.
		log.Printf("bootstrap: %s was created from different settings; replacing it", opts.Name)
		if err := replace(ctx, docker, existing.ID, opts.Name); err != nil {
			return err
		}
	case err == nil && existing.Running:
		log.Printf("bootstrap: %s already running", opts.Name)
		return nil
	case err == nil:
		log.Printf("bootstrap: %s exists but is stopped; starting it", opts.Name)
		if err := docker.StartContainer(ctx, existing.ID); err != nil {
			return fmt.Errorf("start existing %s: %w", opts.Name, err)
		}
		return nil
	case !errors.Is(err, dockerx.ErrContainerNotFound):
		return fmt.Errorf("inspect %s: %w", opts.Name, err)
	}

	// Infrastructure images come from Docker Hub anonymously.
	if err := docker.PullImage(ctx, opts.Image, nil); err != nil {
		return fmt.Errorf("pull %s: %w", opts.Image, err)
	}

	id, err := docker.CreateContainer(ctx, opts)
	if err != nil {
		// Only a name conflict is benign here — it means something
		// created the container between the inspect above and now.
		if isNameConflict(err) {
			log.Printf("bootstrap: %s appeared concurrently; leaving it alone", opts.Name)
			return nil
		}
		return fmt.Errorf("create %s: %w", opts.Name, err)
	}

	if err := docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start %s: %w", opts.Name, err)
	}
	return nil
}

// replace removes an out-of-date container so a new one can take its
// name. A stop that fails is not fatal — the container may already be
// stopped — but a remove that fails is: creating the replacement would
// then collide on the name and be mistaken for a concurrent create.
func replace(ctx context.Context, docker dockerAPI, id, name string) error {
	if err := docker.StopContainer(ctx, id); err != nil {
		log.Printf("bootstrap: could not stop %s before replacing it: %v", name, err)
	}
	if err := docker.RemoveContainer(ctx, id); err != nil {
		return fmt.Errorf("remove the outdated %s: %w", name, err)
	}
	return nil
}

// isNameConflict reports whether err is Docker's "that container name is
// taken" response. The installed SDK (v25.0.6) wraps the daemon's 409 as
// an errdefs.ErrConflict; the message is checked too because the same
// condition reaches us as a plain error through some code paths.
func isNameConflict(err error) bool {
	if errdefs.IsConflict(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already in use") || strings.Contains(msg, "already exists")
}
