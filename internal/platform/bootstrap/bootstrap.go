package bootstrap

import (
	"context"
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

// The daemon's own Postgres, when it isn't pointed at an external one
// with CUBESHIP_DATABASE_URL.
//
// It is published on loopback rather than reached over the "cubeship"
// bridge network because cubeshipd is a host process, not a container —
// the same reason the registry publishes 127.0.0.1:5000. Nothing outside
// this host can connect to it.
const (
	PostgresContainerName = "cubeship-postgres"
	PostgresImage         = "postgres:16-alpine"
	PostgresPort          = 5432
	PostgresUser          = "cubeship"
	PostgresDatabase      = "cubeship"
)

// PostgresDSN is the connection string for the managed Postgres.
// sslmode=disable is correct here and only here: the connection never
// leaves the loopback interface.
func PostgresDSN(password string) string {
	return fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable",
		PostgresUser, url.QueryEscape(password), PostgresPort, PostgresDatabase)
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

func RegistryContainerOpts(cfg *config.Config) dockerx.ContainerOpts {
	labels := traefik.Labels("registry", cfg.RegistryHost, registryPort)
	return dockerx.ContainerOpts{
		Name:    "cubeship-registry",
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
func RegistryConfigYAML(cfg *config.Config, notifyURL, notifyToken string) string {
	realm := "https://" + cfg.APIHost + "/v2/token"
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
func WriteRegistryConfig(cfg *config.Config, notifyURL, notifyToken string) error {
	// Docker would create a missing bind source itself, but only as
	// root-owned; creating it here keeps ownership with the daemon.
	if err := os.MkdirAll(cfg.DataDir+"/registry-data", 0o700); err != nil {
		return fmt.Errorf("create registry data dir: %w", err)
	}
	path := cfg.DataDir + "/registry-config.yml"
	if err := os.WriteFile(path, []byte(RegistryConfigYAML(cfg, notifyURL, notifyToken)), 0o600); err != nil {
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

func TraefikContainerOpts(cfg *config.Config, acmeEmail string) dockerx.ContainerOpts {
	return dockerx.ContainerOpts{
		Name:  "cubeship-traefik",
		Image: "traefik:v3.1",
		Cmd: []string{
			"--providers.docker=true",
			"--providers.docker.exposedbydefault=false",
			"--providers.file.directory=/etc/traefik/dynamic",
			"--providers.file.watch=true",
			"--entrypoints.web.address=:80",
			"--entrypoints.websecure.address=:443",
			"--certificatesresolvers.letsencrypt.acme.tlschallenge=true",
			"--certificatesresolvers.letsencrypt.acme.email=" + acmeEmail,
			"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
			"--api.dashboard=false",
		},
		Binds: []string{
			"/var/run/docker.sock:/var/run/docker.sock:ro",
			cfg.DataDir + "/letsencrypt:/letsencrypt",
			cfg.DataDir + "/traefik-dynamic:/etc/traefik/dynamic",
		},
		HostNetwork: true,
	}
}

// APIRouterConfigYAML returns a Traefik file-provider dynamic
// configuration that routes cfg.APIHost to the daemon's own HTTP API.
// The daemon is a host process (not a container), so it can't be
// discovered via the Docker provider like app containers and the
// registry are — Traefik reaches it over the host-network loopback
// instead. This is what makes the daemon API reachable over HTTPS
// through Traefik, per the spec's architecture and global constraints.
func APIRouterConfigYAML(cfg *config.Config, daemonPort int) string {
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
          - url: "http://127.0.0.1:%d"
`, cfg.APIHost, daemonPort)
}

// WriteAPIRouterConfig writes APIRouterConfigYAML to the path Traefik's
// file provider watches (see the Binds entry above). Call it before
// starting Traefik, and again any time cfg changes.
func WriteAPIRouterConfig(cfg *config.Config, daemonPort int) error {
	dir := cfg.DataDir + "/traefik-dynamic"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create traefik dynamic config dir: %w", err)
	}
	path := dir + "/api.yml"
	if err := os.WriteFile(path, []byte(APIRouterConfigYAML(cfg, daemonPort)), 0o600); err != nil {
		return fmt.Errorf("write traefik dynamic config: %w", err)
	}
	return nil
}

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	InspectContainerByName(ctx context.Context, name string) (string, bool, error)
}

// Ensure makes the described infrastructure container exist and run.
//
// It inspects by name first: an existing running container is left
// alone, an existing stopped one is started (a host reboot otherwise
// leaves the daemon "up" with no proxy running at all), and only a
// genuinely absent one is pulled and created. Creation failures are
// returned rather than assumed to mean "already exists" — a bad bind
// path or port conflict is a real failure, not a no-op.
func Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error {
	existingID, running, err := docker.InspectContainerByName(ctx, opts.Name)
	switch {
	case err == nil && running:
		log.Printf("bootstrap: %s already running", opts.Name)
		return nil
	case err == nil:
		log.Printf("bootstrap: %s exists but is stopped; starting it", opts.Name)
		if err := docker.StartContainer(ctx, existingID); err != nil {
			return fmt.Errorf("start existing %s: %w", opts.Name, err)
		}
		return nil
	case !errors.Is(err, dockerx.ErrContainerNotFound):
		return fmt.Errorf("inspect %s: %w", opts.Name, err)
	}

	if err := docker.PullImage(ctx, opts.Image); err != nil {
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
