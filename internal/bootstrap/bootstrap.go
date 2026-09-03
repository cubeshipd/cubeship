package bootstrap

import (
	"context"
	"fmt"
	"log"
	"os"

	"cubeship/internal/config"
	"cubeship/internal/dockerx"
	"cubeship/internal/traefik"
)

const registryPort = 5000

func RegistryContainerOpts(cfg *config.Config, notifyURL string) dockerx.ContainerOpts {
	labels := traefik.Labels("registry", cfg.RegistryHost, registryPort)
	return dockerx.ContainerOpts{
		Name:   "cubeship-registry",
		Image:  "registry:2",
		Labels: labels,
		Env: []string{
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_NAME=cubeshipd",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_URL=" + notifyURL,
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_TIMEOUT=5s",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_THRESHOLD=5",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_BACKOFF=1s",
		},
		Network: "cubeship",
		// Also published on localhost, plain HTTP, bypassing Traefik/TLS.
		// Docker trusts 127.0.0.0/8 as insecure-by-default, so this needs
		// no daemon.json changes — useful for local pushes and is how
		// Task 20's integration test pushes without needing a real
		// public domain for ACME.
		Ports: []string{"127.0.0.1:5000:5000"},
	}
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
}

func Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error {
	if err := docker.PullImage(ctx, opts.Image); err != nil {
		return fmt.Errorf("pull %s: %w", opts.Image, err)
	}

	id, err := docker.CreateContainer(ctx, opts)
	if err != nil {
		log.Printf("bootstrap: create %s: %v (assuming it already exists and is running)", opts.Name, err)
		return nil
	}

	if err := docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start %s: %w", opts.Name, err)
	}
	return nil
}
