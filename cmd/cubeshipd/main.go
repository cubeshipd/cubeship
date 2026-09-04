package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/config"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/server"
	"cubeship/internal/settings"
	"cubeship/internal/user"

	_ "github.com/jackc/pgx/v5/stdlib"
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

// Secrets the daemon generates for itself on first start, each persisted
// under the data dir at mode 0600 — the same treatment the daemon token
// gets in config.Load.
const (
	// adminKeyFileName holds the super-admin's API key.
	//
	// This is deliberately NOT cfg.Token. That token is an instance-wide
	// system credential: the registry webhook's shared secret. Registry
	// push/pull now goes through per-user tokens (internal/regauth), so
	// there's no longer any reason cfg.Token would need to double as
	// anyone's API key — but the separation predates that and stays
	// correct regardless: seeding the super-admin's API key from a
	// system-wide secret would mean anyone who obtained it could create
	// orgs, create admins anywhere, and read every app's environment.
	adminKeyFileName = "admin-api-key"

	// pgPasswordFileName holds the managed Postgres' password. It is
	// generated once and reused: Postgres only reads POSTGRES_PASSWORD
	// when it initializes an empty data directory, so a password
	// regenerated on restart would simply stop matching the database.
	pgPasswordFileName = "postgres-password"
)

// loadOrCreateSecret returns the secret stored in dataDir/name and the
// file it lives in, generating and persisting one on first call.
func loadOrCreateSecret(dataDir, name string) (string, string, error) {
	path := filepath.Join(dataDir, name)
	data, err := os.ReadFile(path)
	if err == nil {
		if secret := strings.TrimSpace(string(data)); secret != "" {
			return secret, path, nil
		}
		// An empty file (a truncated write from an earlier crash) is
		// treated as no secret at all and replaced below.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", fmt.Errorf("read %s: %w", path, err)
	}

	secret, err := authkey.Generate()
	if err != nil {
		return "", "", fmt.Errorf("generate %s: %w", name, err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write %s: %w", path, err)
	}
	return secret, path, nil
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
func ensureSuperAdmin(ctx context.Context, users *user.Service, dataDir string) error {
	n, err := users.Repo().Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	key, path, err := loadOrCreateSecret(dataDir, adminKeyFileName)
	if err != nil {
		return err
	}

	var username string
	if err := users.DB().WithTx(ctx, func(tx database.Queryer) error {
		repo := user.NewRepository(tx)
		created, err := repo.Create(ctx, "admin", true)
		if err != nil {
			return err
		}
		username = created.Username
		_, err = repo.CreateAPIKey(ctx, created.ID, authkey.Hash(key), user.DefaultAPIKeyName)
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

// databaseReadyTimeout bounds how long the daemon waits for a
// just-started Postgres to accept connections. A container initializing
// an empty data directory runs initdb first, which on a small VPS is
// comfortably slower than the store's own connect timeout.
const databaseReadyTimeout = 2 * time.Minute

// ensureDatabase returns the DSN the store should open, bringing up the
// daemon's own Postgres container first when no external database was
// configured.
//
// An external CUBESHIP_DATABASE_URL is used as-is and never managed: the
// operator owns that server's lifecycle, its backups and its version.
func ensureDatabase(ctx context.Context, cfg *config.Config, docker *dockerx.Client) (string, error) {
	if !cfg.ManagedDatabase() {
		log.Printf("using the Postgres at CUBESHIP_DATABASE_URL; this daemon does not manage it")
		return cfg.DatabaseURL, nil
	}

	password, path, err := loadOrCreateSecret(cfg.DataDir, pgPasswordFileName)
	if err != nil {
		return "", err
	}
	if err := bootstrap.EnsurePostgresDataDir(cfg); err != nil {
		return "", err
	}
	if err := bootstrap.Ensure(ctx, docker, bootstrap.PostgresContainerOpts(cfg, password)); err != nil {
		return "", fmt.Errorf("bootstrap postgres: %w", err)
	}
	log.Printf("managed Postgres in container %s; its password (fingerprint %s) is stored in %s",
		bootstrap.PostgresContainerName, config.TokenFingerprint(password), path)

	dsn := bootstrap.PostgresDSN(password)
	if err := waitForDatabase(ctx, dsn, databaseReadyTimeout); err != nil {
		return "", err
	}
	return dsn, nil
}

// waitForDatabase blocks until the database accepts a connection. A
// container that has just been created is not ready the instant Docker
// reports it running — Postgres still has to initialize and start
// listening — so connecting immediately would fail on every first boot.
func waitForDatabase(ctx context.Context, dsn string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return fmt.Errorf("open database: %w", err)
		}
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		lastErr = db.PingContext(pingCtx)
		cancel()
		db.Close()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("database did not become ready within %s: %w", timeout, lastErr)
}

// applyInfrastructure brings the containers that depend on instance
// configuration into line with it.
//
// The registry is only started once a domain exists: its token realm has
// to be an address a remote `docker push` can reach, and there is no such
// address before then. Traefik always runs — it routes apps by their own
// domains, which have nothing to do with the instance's — but only gains
// a certificate resolver once a contact address is configured.
//
// Both are idempotent, and bootstrap.Ensure replaces a container whose
// settings have changed, so calling this again after a settings change is
// all it takes to apply one.
func applyInfrastructure(ctx context.Context, cfg *config.Config, docker *dockerx.Client, values settings.Values) error {
	if values.HasDomain() {
		apiHost := settings.APIHostFor(values.Get(settings.Domain))

		// The registry container runs on the "cubeship" bridge network,
		// not the host's namespace, so it reaches the daemon via
		// host.docker.internal rather than 127.0.0.1 (see ExtraHosts in
		// RegistryContainerOpts).
		notifyURL := fmt.Sprintf("http://host.docker.internal:%d/hooks/registry", daemonPort)
		if err := bootstrap.WriteRegistryConfig(cfg, apiHost, notifyURL, cfg.Token); err != nil {
			return fmt.Errorf("write registry config: %w", err)
		}
		registryHost := settings.RegistryHostFor(values.Get(settings.Domain))
		if err := bootstrap.Ensure(ctx, docker,
			bootstrap.RegistryContainerOpts(cfg, registryHost, values.HasTLS())); err != nil {
			return fmt.Errorf("bootstrap registry: %w", err)
		}
		if err := bootstrap.WriteAPIRouterConfig(cfg, apiHost, daemonPort); err != nil {
			return fmt.Errorf("write traefik API router config: %w", err)
		}
	}

	if err := bootstrap.Ensure(ctx, docker,
		bootstrap.TraefikContainerOpts(cfg, values.Get(settings.ACMEEmail))); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}
	return nil
}

// sessionPurgeInterval is how often expired sessions are swept up.
// Expiry already takes effect at lookup — a session past its date
// resolves to nobody — so this is only housekeeping, and an hour is
// often enough to stop the table growing without being work anyone
// notices.
const sessionPurgeInterval = time.Hour

func purgeExpiredSessions(ctx context.Context, users *user.Service) {
	ticker := time.NewTicker(sessionPurgeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := users.PurgeExpiredSessions(ctx)
			if err != nil {
				// Nothing is broken for anyone: expired sessions are
				// already rejected. Say so and try again next hour.
				log.Printf("could not purge expired sessions: %v", err)
				continue
			}
			if n > 0 {
				log.Printf("purged %d expired session(s)", n)
			}
		}
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log.Printf("cubeshipd starting")
	// Never log the token itself — the daemon's logs are not a secret
	// store. A fingerprint is enough to tell which token is in use.
	// This is the instance-wide system credential for the registry's
	// push-notification webhook, not anyone's API key or a registry
	// login credential (registry push/pull now goes through per-user
	// tokens; see adminKeyFileName and internal/regauth).
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

	dsn, err := ensureDatabase(ctx, cfg, docker)
	if err != nil {
		return err
	}
	db, err := database.Open(dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	srv := server.New(db, docker, server.Options{WebhookToken: cfg.Token})

	// An install upgrading from the release where the domain and contact
	// address were required environment variables keeps them, once.
	if err := srv.Settings.SeedFromEnv(ctx, config.SeedSettings()); err != nil {
		return fmt.Errorf("carry the old environment into settings: %w", err)
	}

	if err := ensureSuperAdmin(ctx, srv.Users, cfg.DataDir); err != nil {
		return fmt.Errorf("bootstrap super-admin: %w", err)
	}

	registryCert, err := regauth.SelfSignedCert(registrySigningKey, "cubeship")
	if err != nil {
		return fmt.Errorf("create registry token certificate: %w", err)
	}
	if err := bootstrap.WriteRegistryTokenCert(cfg, registryCert); err != nil {
		return fmt.Errorf("write registry token certificate: %w", err)
	}

	// applyInfrastructure is run now with whatever is configured, and
	// again whenever the operator changes it — adding a domain has to
	// bring the registry up without a restart.
	apply := func(ctx context.Context, values settings.Values) error {
		return applyInfrastructure(ctx, cfg, docker, values)
	}
	current, err := srv.Settings.Load(ctx)
	if err != nil {
		return fmt.Errorf("read instance settings: %w", err)
	}
	if err := apply(ctx, current); err != nil {
		return err
	}
	srv.Settings.OnChange(apply)

	if err := app.Reconcile(ctx, srv.Apps.Repo(), docker); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	srv.SetRegistrySigningKey(registrySigningKey)

	go purgeExpiredSessions(ctx, srv.Users)

	log.Printf("cubeshipd listening on %s", listenAddr)
	if !current.HasDomain() {
		log.Printf("no domain configured yet: apps can be created, but there is no registry to push to until one is set")
	}
	return http.ListenAndServe(listenAddr, srv.Router())
}
