package bootstrap

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/platform/traefik"
)

const registryPort = 5000

// TraefikContainerName is the reverse proxy. It is exported because it
// is also the only place an ACME failure is recorded: certificates reads
// its log to say why a name has no certificate.
const TraefikContainerName = "cubeship-traefik"

// RegistryContainerName is the embedded registry, and the name the
// daemon pulls from when both are containers.
const RegistryContainerName = "cubeship-registry"

// Network is the application bridge. Private infrastructure uses
// dockerx.ManagementNetwork; Traefik joins both to reach their backends.
const Network = dockerx.ApplicationNetwork

// DaemonContainerName is what the daemon is called when it runs as a
// container, and so the name everything else reaches it by.
const DaemonContainerName = "cubeship-daemon"

// localDaemonPort is the port the daemon answers on. It is duplicated
// from cmd/cubeshipd rather than imported, because a package the daemon
// depends on cannot depend back on it — and it appears here only in a
// fallback address for a registry with no domain.
const localDaemonPort = 3000

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

// LocalRegistryAddress is where **the daemon** reaches the registry's own
// API: the catalogue, a delete, a garbage-collection pass.
//
// Never the public name: that would hairpin out to this host's own
// address and need a certificate to already exist, which must not be
// what a deploy waits on. The daemon is a container on the `cubeship`
// bridge, so the registry's container name is what resolves for it —
// through Docker's embedded DNS, which only containers on that network
// can ask.
//
// **That last sentence is why PullRegistryAddress exists.** See it
// before using this for anything that ends up in an image reference.
func LocalRegistryAddress(cfg *config.Config) string {
	if cfg.InContainer {
		return fmt.Sprintf("%s:%d", RegistryContainerName, registryPort)
	}
	return fmt.Sprintf("127.0.0.1:%d", registryPort)
}

// PullRegistryAddress is where **the Engine** pulls an app's image from,
// and it is not the same answer.
//
// An image reference is resolved by the Engine, which is the host's
// daemon and is on no user-defined network. Container names live in
// Docker's embedded DNS, which only containers on that network can ask
// — so a reference beginning `cubeship-registry:5000` is one the Engine
// looks up through the *host's* resolver and never finds. The pull
// fails with "server misbehaving", which reads as a broken registry and
// is a name that was never going to resolve where it was sent.
//
// So this is loopback whatever the daemon is, because the published
// port is on the host either way and the Engine is always the host's.
// Docker trusts 127.0.0.0/8 as insecure by default, which is what keeps
// this from needing a certificate or a daemon.json entry.
//
// The address is also the key the daemon's own token signer is
// registered under, so the two have to be the same string: a token
// minted for one host is not attached to a pull from another.
func PullRegistryAddress() string {
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
		Network: dockerx.ManagementNetwork,
	}
}

// PostgresMajor is the major version this instance's own database
// runs, read off the image tag so the two cannot disagree.
func PostgresMajor() string {
	_, tag, ok := strings.Cut(PostgresImage, ":")
	if !ok {
		return ""
	}
	major, _, _ := strings.Cut(tag, "-")
	return major
}

// DumpPostgres streams a logical dump of Cubeship's own database.
//
// **Through the container, because the daemon has no `pg_dump`.** Its
// image is Alpine with one binary in it; Postgres's own image has the
// tool, is already running, and is already the thing that would have to
// be reachable for this to mean anything.
//
// **The password is read inside the container, not passed in.** It is
// already in that container's environment — the image was started with
// it — so the command is a fixed string with nothing interpolated into
// it, which is the rule `firewall.Spec.Args` keeps for the same reason.
// A secret on an argv is a secret in `ps`.
//
// Plain SQL rather than the custom format: what this is for is an
// operator putting an instance back, and `psql < cubeship.sql` is a
// thing somebody can do at three in the morning without reading a
// manual. It compresses on the way into the archive anyway.
func DumpPostgres(ctx context.Context, docker PostgresDumper, w io.Writer) (string, error) {
	stderr, code, err := docker.ExecStream(ctx, PostgresContainerName,
		[]string{"sh", "-c",
			`PGPASSWORD="$POSTGRES_PASSWORD" pg_dump --no-owner --no-privileges ` +
				`-U "$POSTGRES_USER" -d "$POSTGRES_DB"`},
		nil, w)
	if err != nil {
		return stderr, err
	}
	if code != 0 {
		return stderr, fmt.Errorf("pg_dump exited %d", code)
	}
	return stderr, nil
}

// PostgresDumper is the one thing DumpPostgres needs of the Engine.
type PostgresDumper interface {
	ExecStream(ctx context.Context, container string, cmd []string, r io.Reader, w io.Writer) (stderr string, exitCode int, err error)
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
func RegistryContainerOpts(cfg *config.Config, registryHost string, tls bool, tokenCert []byte) dockerx.ContainerOpts {
	// No domain, no router: a Traefik rule with an empty host matches
	// nothing and is worth less than not existing. The registry still
	// runs — the daemon pulls from it directly, and a push from this
	// host reaches it on loopback — it is only unreachable from
	// elsewhere until there is a name for it.
	var labels map[string]string
	if registryHost != "" {
		labels = traefik.Labels("registry", []traefik.Domain{{Host: registryHost, Port: registryPort}}, tls, "")
		labels["traefik.docker.network"] = dockerx.ManagementNetwork
	}
	// The trust root goes into the container's own labels, and it has to.
	//
	// The registry reads its rootcertbundle once, when it starts, and
	// accepts only that exact certificate. Nothing else in these options
	// changes when the certificate does — same image, same binds, same
	// path — so Ensure saw an unchanged container and left a registry
	// running that trusted a certificate the daemon no longer signs
	// with. Every token was refused, with "unable to get token signing
	// key" in the registry's log and a bare 401 everywhere else.
	//
	// A fingerprint rather than the certificate: a label is metadata a
	// person reads with `docker inspect`, not a place to put a PEM.
	if labels == nil {
		labels = map[string]string{}
	}
	labels[TokenCertLabel] = fingerprint(tokenCert)

	return dockerx.ContainerOpts{
		Name:    RegistryContainerName,
		Image:   "registry:2",
		Labels:  labels,
		Network: dockerx.ManagementNetwork,
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
// htpasswd credential) is what authorizes each push through the
// registry token handler and internal/platform/regauth signing.
//
// notifyToken is the daemon's system token (cfg.Token). It is the
// shared secret on the notification endpoint's Authorization header, so
// the deliberately-unauthenticated /hooks/registry route can still tell
// a genuine push notification from a forged one — it has nothing to do
// with registry push/pull authentication, which is entirely the token
// realm's job now.
func RegistryConfigYAML(apiHost, notifyURL, notifyToken string) string {
	// Where a client is told to go for a token. With a domain it is the
	// public API; without one there is no public name yet, and the only
	// address that works is this host's own — which is enough for a push
	// from here, and is what the integration test uses.
	realm := "https://" + apiHost + "/v2/token"
	if apiHost == "" {
		realm = fmt.Sprintf("http://127.0.0.1:%d/v2/token", localDaemonPort)
	}
	return fmt.Sprintf(`version: 0.1
log:
  fields:
    service: registry
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /var/lib/registry
  delete:
    enabled: true
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
