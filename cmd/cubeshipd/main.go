package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"cubeship/internal/api"
	"cubeship/internal/authkey"
	"cubeship/internal/bootstrap"
	"cubeship/internal/config"
	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/reconcile"
	"cubeship/internal/store"
)

const version = "0.1.0-dev"
const daemonPort = 9000

// localRegistryHost is the loopback address the registry container
// publishes, and the host the daemon's own image pulls target. It must
// match internal/api's constant of the same name.
const localRegistryHost = "127.0.0.1:5000"

// listenAddr binds all interfaces on purpose: the registry container
// reaches the webhook through host.docker.internal, which resolves to
// the host's bridge-gateway address, not loopback — binding 127.0.0.1
// would cut that path.
//
// This port MUST NOT be exposed to the public internet by the host
// firewall. The API is meant to be reached over HTTPS through Traefik
// (api.<domain>); :9000 additionally serves the same API in plaintext.
// See README.md.
const listenAddr = ":9000"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeshipd %s\n", version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// ensureSuperAdmin creates the instance's first user — a super-admin —
// the first time the daemon boots against a fresh database, seeding
// their API key from token (cfg.Token, the same persisted/generated
// secret config.Load already manages). A database that already has any
// users is left alone.
func ensureSuperAdmin(ctx context.Context, s *store.Store, token string) error {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	user, err := s.CreateUser(ctx, "admin", true)
	if err != nil {
		return err
	}
	if _, err := s.CreateAPIKey(ctx, user.ID, authkey.Hash(token)); err != nil {
		return err
	}
	log.Printf("cubeshipd: created super-admin user %q, API key seeded from the daemon token", user.Username)
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log.Printf("cubeshipd starting for domain %s", cfg.Domain)
	// Never log the token itself — the daemon's logs are not a secret
	// store. A fingerprint is enough to tell which token is in use.
	if cfg.TokenFile != "" {
		log.Printf("daemon API token (fingerprint %s) is stored in %s", config.TokenFingerprint(cfg.Token), cfg.TokenFile)
	} else {
		log.Printf("daemon API token (fingerprint %s) taken from CUBESHIP_TOKEN", config.TokenFingerprint(cfg.Token))
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	// The embedded registry requires basic auth (see
	// bootstrap.RegistryConfigYAML); the daemon pulls from it over
	// loopback with the same credentials the CLI's `registry login`
	// uses.
	docker.SetRegistryAuth(localRegistryHost, bootstrap.RegistryUsername, cfg.Token)

	ctx := context.Background()

	if err := docker.EnsureNetwork(ctx, "cubeship"); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	s, err := store.Open(cfg.DataDir + "/cubeship.db")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	if err := ensureSuperAdmin(ctx, s, cfg.Token); err != nil {
		return fmt.Errorf("bootstrap super-admin: %w", err)
	}

	// The registry container that POSTs to this URL runs on the "cubeship"
	// bridge network, not the host's network namespace, so it must reach
	// the daemon via host.docker.internal rather than 127.0.0.1 (see the
	// registry container's ExtraHosts in bootstrap.RegistryContainerOpts).
	notifyURL := fmt.Sprintf("http://host.docker.internal:%d/hooks/registry", daemonPort)
	if err := bootstrap.WriteRegistryConfig(cfg, notifyURL, cfg.Token); err != nil {
		return fmt.Errorf("write registry config: %w", err)
	}
	if err := bootstrap.WriteRegistryHtpasswd(cfg, cfg.Token); err != nil {
		return fmt.Errorf("write registry credentials: %w", err)
	}
	if err := bootstrap.Ensure(ctx, docker, bootstrap.RegistryContainerOpts(cfg)); err != nil {
		return fmt.Errorf("bootstrap registry: %w", err)
	}
	if err := bootstrap.WriteAPIRouterConfig(cfg, daemonPort); err != nil {
		return fmt.Errorf("write traefik API router config: %w", err)
	}
	if err := bootstrap.Ensure(ctx, docker, bootstrap.TraefikContainerOpts(cfg, cfg.AcmeEmail)); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}

	if err := reconcile.Run(ctx, s, docker); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	orch := deploy.NewOrchestrator(s, docker)
	srv := api.NewServer(s, orch, cfg.Token, cfg.RegistryHost)

	log.Printf("cubeshipd listening on %s", listenAddr)
	return http.ListenAndServe(listenAddr, srv.Router())
}
