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
		Binds: []string{cfg.DataDir + "/registry-config.yml:/etc/docker/registry/config.yml:ro"},
	}
}

// RegistryConfigYAML returns the registry:2 config.yml this daemon
// needs: the image's own default settings (storage, http, health),
// plus a notifications.endpoint pointing at the daemon's webhook. This
// replaces (not merges with) the image's baked-in config, so it must
// carry everything the registry needs to run, not just the
// notifications section.
func RegistryConfigYAML(notifyURL string) string {
	return fmt.Sprintf(`version: 0.1
log:
  fields:
    service: registry
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /var/lib/registry
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
      timeout: 5s
      threshold: 5
      backoff: 1s
`, notifyURL)
}

// WriteRegistryConfig writes RegistryConfigYAML to the path
// RegistryContainerOpts' bind mount expects. Call it before starting
// the registry container.
func WriteRegistryConfig(cfg *config.Config, notifyURL string) error {
	path := cfg.DataDir + "/registry-config.yml"
	if err := os.WriteFile(path, []byte(RegistryConfigYAML(notifyURL)), 0o600); err != nil {
		return fmt.Errorf("write registry config: %w", err)
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
