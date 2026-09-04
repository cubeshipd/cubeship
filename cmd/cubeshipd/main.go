package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cubeship/internal/api"
	"cubeship/internal/authkey"
	"cubeship/internal/bootstrap"
	"cubeship/internal/config"
	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/reconcile"
	"cubeship/internal/regauth"
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

// adminKeyFileName is where the super-admin's API key is persisted,
// under the data dir, mode 0600 — the same treatment the daemon token
// gets in config.Load.
const adminKeyFileName = "admin-api-key"

// loadOrCreateAdminKey returns the super-admin's API key and the file it
// lives in, generating and persisting one on first call.
//
// This is deliberately NOT cfg.Token. That token is an instance-wide
// system credential: the registry webhook's shared secret. Registry
// push/pull now goes through per-user tokens (internal/regauth), so
// there's no longer any reason cfg.Token would need to double as
// anyone's API key — but the separation predates that and stays
// correct regardless: seeding the super-admin's API key from a
// system-wide secret would mean anyone who obtained it could create
// orgs, create admins anywhere, and read every app's environment.
func loadOrCreateAdminKey(dataDir string) (string, string, error) {
	path := filepath.Join(dataDir, adminKeyFileName)
	data, err := os.ReadFile(path)
	if err == nil {
		if key := strings.TrimSpace(string(data)); key != "" {
			return key, path, nil
		}
		// An empty file (a truncated write from an earlier crash) is
		// treated as no key at all and replaced below.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read admin API key %s: %w", path, err)
	}

	key, err := authkey.Generate()
	if err != nil {
		return "", "", fmt.Errorf("generate admin API key: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write admin API key %s: %w", path, err)
	}
	return key, path, nil
}

// ensureSuperAdmin creates the instance's first user — a super-admin —
// the first time the daemon boots against a fresh database, with its own
// generated API key persisted under dataDir. A database that already has
// any users is left alone.
//
// The user and their key are created in one transaction: a user that
// exists with no key would take the username, block the bootstrap
// (which only runs while there are no users at all) and leave the
// instance with no way in.
func ensureSuperAdmin(ctx context.Context, s *store.Store, dataDir string) error {
	n, err := s.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	key, path, err := loadOrCreateAdminKey(dataDir)
	if err != nil {
		return err
	}

	var username string
	if err := s.WithTx(ctx, func(tx *store.Tx) error {
		user, err := tx.CreateUser(ctx, "admin", true)
		if err != nil {
			return err
		}
		username = user.Username
		_, err = tx.CreateAPIKey(ctx, user.ID, authkey.Hash(key))
		return err
	}); err != nil {
		return err
	}

	// The key itself stays out of the log, like the daemon token: a
	// fingerprint identifies it, and the file is where the operator
	// reads it from.
	log.Printf("cubeshipd: created super-admin user %q; its API key (fingerprint %s) is stored in %s",
		username, config.TokenFingerprint(key), path)
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
	// This is the instance-wide system credential for the registry's
	// push-notification webhook, not anyone's API key or a registry
	// login credential (registry push/pull now goes through per-user
	// tokens; see loadOrCreateAdminKey and internal/regauth).
	if cfg.TokenFile != "" {
		log.Printf("daemon webhook token (fingerprint %s) is stored in %s", config.TokenFingerprint(cfg.Token), cfg.TokenFile)
	} else {
		log.Printf("daemon webhook token (fingerprint %s) taken from CUBESHIP_TOKEN", config.TokenFingerprint(cfg.Token))
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	registrySigningKey, err := regauth.LoadOrCreateKeyPair(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("load registry signing key: %w", err)
	}

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	// The daemon's own pulls need no HTTP round-trip through /v2/token:
	// it already holds the private key in-process, so it mints a
	// pull-only token for exactly the repository being pulled, fresh
	// every time (tokens expire in regauth.TokenTTL).
	docker.SetRegistryTokenSigner(localRegistryHost, func(repository string) (string, error) {
		return regauth.IssueToken(registrySigningKey, regauth.TokenIssuer, regauth.TokenService, "cubeshipd",
			[]regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}})
	})

	ctx := context.Background()

	if err := docker.EnsureNetwork(ctx, "cubeship"); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	s, err := store.Open(cfg.DataDir + "/cubeship.db")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	if err := ensureSuperAdmin(ctx, s, cfg.DataDir); err != nil {
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
	registryCert, err := regauth.SelfSignedCert(registrySigningKey, "cubeship")
	if err != nil {
		return fmt.Errorf("create registry token certificate: %w", err)
	}
	if err := bootstrap.WriteRegistryTokenCert(cfg, registryCert); err != nil {
		return fmt.Errorf("write registry token certificate: %w", err)
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
	srv.SetRegistrySigningKey(registrySigningKey)

	log.Printf("cubeshipd listening on %s", listenAddr)
	return http.ListenAndServe(listenAddr, srv.Router())
}
