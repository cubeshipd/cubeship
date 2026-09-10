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
	"cubeship/internal/datastore"
	"cubeship/internal/machine"
	"cubeship/internal/metrics"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/config"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/server"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/update"
	"cubeship/internal/user"
	"cubeship/internal/worker"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// version is stamped at link time by `make release`; a build without
// that says so rather than claiming a number nobody released.
var version = "dev"

// daemonPort is the front door. It is 3000 because on a fresh box that
// is the whole product: no domain exists yet, so there is no HTTPS and
// no api.<domain> — the installer tells you to open http://<ip>:3000 and
// that has to be the dashboard.
const daemonPort = 3000

// listenAddr binds all interfaces on purpose, for two reasons: the
// registry container reaches the webhook through host.docker.internal,
// which resolves to the host's bridge-gateway address rather than
// loopback, and an operator with no domain yet reaches the dashboard
// here from their own machine.
//
// It is plaintext. Once a domain exists everything is reachable over
// HTTPS through Traefik at api.<domain>, and this port has no remaining
// use from outside — close it at the host firewall then. See README.md.
var listenAddr = fmt.Sprintf(":%d", daemonPort)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	// The one mode that is not a daemon at all: a throwaway container
	// started by the daemon it is about to replace. See replaceDaemon.
	replace := flag.String("replace", "", "replace this container with a new image and exit")
	replaceImage := flag.String("replace-image", "", "the image to replace it with")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeshipd %s\n", version)
		os.Exit(0)
	}

	if *replace != "" {
		if err := replaceDaemon(*replace, *replaceImage); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// replaceDaemon stops the daemon's container and starts it again from a
// new image.
//
// **It is a container of its own, and it has to be**: the process it
// stops is the one that asked for this, so nothing running inside that
// container could see the operation through. This runs from the new
// image, started with the Docker socket and the data directory, and
// does one thing.
//
// The options are read back off the container being replaced rather
// than written here. They were chosen by whoever installed this — a
// port, a domain, a data directory somewhere unusual — and a second
// copy of them in this binary would be one that goes stale the first
// time install.sh grows a flag.
//
// What it cannot do is report a failure to the daemon, because there is
// no daemon while it runs. So it writes the status file itself, which
// is the same file a browser is reading to know to stay out of the way.
func replaceDaemon(name, image string) error {
	ctx, cancel := context.WithTimeout(context.Background(), update.StuckAfter)
	defer cancel()

	store := update.Store{DataDir: config.DataDir()}
	run := store.Read()
	if run == nil {
		run = &update.Run{Version: version, Status: update.StatusRunning, StartedAt: time.Now()}
	}

	docker, err := dockerx.New()
	if err != nil {
		store.Finish(run, err)
		return err
	}
	log.Printf("update: replacing %s with %s", name, image)

	spec, err := docker.SpecOf(ctx, name)
	if err != nil {
		store.Finish(run, fmt.Errorf("read %s's own settings: %w", name, err))
		return err
	}
	spec.Image = image
	// Every other variable is carried over as it stands. This one names
	// the dashboard's image, and a daemon that comes back still
	// pointing at the old one puts the old dashboard back the next time
	// it starts — an update that looks done and undoes itself on the
	// next reboot.
	spec.Env = update.WebImageEnv(spec.Env, run.Version)

	if err := docker.StopContainer(ctx, name); err != nil {
		log.Printf("update: stopping %s: %v", name, err)
	}
	if err := docker.RemoveContainer(ctx, name); err != nil {
		store.Finish(run, fmt.Errorf("remove %s: %w", name, err))
		return err
	}
	id, err := docker.CreateContainer(ctx, spec)
	if err != nil {
		// The daemon is gone and its replacement could not be made,
		// which is the one failure here that leaves an instance down.
		// Said as loudly as a status file can say anything.
		store.Finish(run, fmt.Errorf("create %s again: %w — the instance is down, and `docker run` from install.sh is what brings it back", name, err))
		return err
	}
	if err := docker.StartContainer(ctx, id); err != nil {
		store.Finish(run, fmt.Errorf("start %s again: %w", name, err))
		return err
	}
	// Deliberately not marked done here: what finishes this run is the
	// daemon that comes back reporting the version it was moving to.
	// Saying so from here would be this container's opinion about a
	// process it has not seen start.
	log.Printf("update: %s is running %s", name, image)
	return nil
}

// runWorker is the whole of a worker's daemon.
//
// No database is opened, no migration runs, no HTTP server is started
// and no port is published: a worker's entire network presence is an
// outbound call to its control plane. What it needs is the Engine — for
// the containers it will be told to run — the machine's own numbers,
// and a way to work out where it is reached from outside.
func runWorker(cfg *config.Config) error {
	ctx := context.Background()

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	// The shared network exists on every machine in the cluster, not
	// only on the control plane: it is what the containers placed here
	// will join, and creating it now means the first placement does not
	// have to.
	if err := docker.EnsureNetwork(ctx, bootstrap.Network); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	box := machine.NewReader(cfg.DataDir, cfg.InContainer)

	// Where this machine is reached from outside, through the same door
	// the control plane finds its own address through: a command in the
	// host's namespaces, and a private address refused rather than
	// reported. What a worker's address is *for* is the same thing —
	// a DNS record pointing at the box an app runs on.
	host := hostexec.NewRunner(docker, bootstrap.OwnImage(ctx, docker, cfg), cfg.InContainer)
	var address worker.HostAddress
	if host.Available() {
		address = settings.RouteAddress(func(ctx context.Context, argv ...string) (string, error) {
			res, err := host.Run(ctx, argv...)
			if err != nil {
				return "", err
			}
			if !res.OK() {
				return "", fmt.Errorf("%s: exit %d", argv[0], res.Code)
			}
			return res.Output, nil
		})
	}

	// A worker runs no proxy at all. Every name this instance serves
	// arrives at the control plane, which routes it here over the mesh
	// by container name — so there is nothing on this box to terminate
	// TLS, no certificate store, and no port to hold open.
	log.Printf("worker mode: this machine belongs to %s and serves nothing of its own", cfg.ControlPlane)
	worker.New(cfg.ControlPlane, cfg.NodeToken, version, cfg.DataDir, box, docker, address, host).Run(ctx)
	return nil
}

// Secrets the daemon generates for itself on first start, persisted
// under the data dir at mode 0600 — the same treatment the daemon token
// gets in config.Load.
//
// The super-admin's API key used to be one of these. It no longer exists:
// the first account arrives through setup, in a browser, and its way in
// is a password.
const (
	// pgPasswordFileName holds the managed Postgres' password. It is
	// generated once and reused: Postgres only reads POSTGRES_PASSWORD
	// when it initializes an empty data directory, so a password
	// regenerated on restart would simply stop matching the database.
	pgPasswordFileName = "postgres-password"

	// builderTokenFileName holds what a build logs in to this instance's
	// own registry as when it pushes. Generated once and kept, like
	// every other secret here, so an operator can see what is holding a
	// push open — and separate from the daemon's token because what it
	// may do is different: push any image, rather than say one was
	// pushed. See app.BuilderUsername.
	builderTokenFileName = "builder-token"
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

	dsn := bootstrap.PostgresDSN(cfg, password)
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
func applyInfrastructure(ctx context.Context, cfg *config.Config, docker *dockerx.Client, values settings.Values, tokenCert []byte) error {
	// The registry always runs. It is part of what Cubeship is, not
	// something a domain switches on: the daemon pulls an app's own
	// image from it, and a push from this host reaches it on loopback.
	// What a domain adds is a name the rest of the world can push to.
	apiHost := settings.APIHostFor(values.Get(settings.Domain))
	registryHost := settings.RegistryHostFor(values.Get(settings.Domain))

	// The registry container runs on the "cubeship" bridge network, so
	// it reaches the daemon by the address DaemonAddress gives — a
	// container name, or the host gateway when the daemon is a host
	// process.
	notifyURL := fmt.Sprintf("http://%s/hooks/registry", bootstrap.DaemonAddress(cfg, daemonPort))
	if err := bootstrap.WriteRegistryConfig(cfg, apiHost, notifyURL, cfg.Token); err != nil {
		return fmt.Errorf("write registry config: %w", err)
	}
	if err := bootstrap.Ensure(ctx, docker,
		bootstrap.RegistryContainerOpts(cfg, registryHost, values.HasTLS(), tokenCert)); err != nil {
		return fmt.Errorf("bootstrap registry: %w", err)
	}

	if values.HasDomain() {
		if err := bootstrap.WriteAPIRouterConfig(cfg, apiHost, daemonPort); err != nil {
			return fmt.Errorf("write traefik API router config: %w", err)
		}
	}

	if err := bootstrap.Ensure(ctx, docker,
		bootstrap.TraefikContainerOpts(cfg, values.HasTLS(), values.Get(settings.ACMEEmail))); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}

	// The dashboard. Its own container, which the daemon proxies to for
	// everything that is not /api — so the instance is one address and
	// a session cookie is same-origin for both halves.
	//
	// A failure here does not stop the daemon: the API, the CLI and
	// every running app are unaffected by a dashboard that is down, and
	// refusing to start over it would turn a cosmetic problem into an
	// outage. internal/web says so at the address instead.
	if err := bootstrap.EnsureFrontend(ctx, docker, cfg); err != nil {
		log.Printf("bootstrap: the dashboard did not start: %v", err)
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

	// A worker is a different program, and this is where the two part.
	//
	// Everything below this line — the database, the registry's signing
	// key, the settings, the server, the listener — is the control
	// plane's. A machine that belongs to another instance holds none of
	// it: it dials out, says what it is, and does what it is told.
	if cfg.Worker() {
		return runWorker(cfg)
	}
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
	// One certificate, made here and used in both places it has to
	// match: the registry's trust root on disk, and the x5c header of
	// every token issued. Two certificates over one key would leave
	// whether the registry accepts the pairing up to how it builds its
	// pool, rather than to anything decided here.
	registryCert, registryCertDER, err := regauth.SelfSignedCert(registrySigningKey, "cubeship")
	if err != nil {
		return fmt.Errorf("create registry token certificate: %w", err)
	}

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	// The daemon's own pulls need no HTTP round-trip through /v2/token:
	// it already holds the private key in-process, so it mints a
	// pull-only token for exactly the repository being pulled, fresh
	// every time (tokens expire in regauth.TokenTTL).
	localRegistry := bootstrap.LocalRegistryAddress(cfg)
	docker.SetRegistryTokenSigner(localRegistry, func(repository string) (string, error) {
		return regauth.IssueToken(registrySigningKey, registryCertDER, regauth.TokenIssuer, regauth.TokenService, "cubeshipd",
			[]regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}})
	})

	ctx := context.Background()

	if err := docker.EnsureNetwork(ctx, bootstrap.Network); err != nil {
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

	// The builder starts buildkitd the first time something needs it.
	// An instance that only runs images it is given never pays for a
	// privileged container it will not use.
	builder := buildkit.New(bootstrap.BuildKitSocket(cfg), docker)
	builder.EnsureRunning = func(ctx context.Context) error {
		return bootstrap.EnsureBuildKit(ctx, docker, cfg)
	}

	// Written before the server exists, because the setup endpoint is
	// built around whether there is one.
	setupToken, err := setup.EnsureToken(ctx, db, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("prepare the setup token: %w", err)
	}

	// What lets the firewall module reach the host it is a firewall
	// for. It runs a throwaway container from this daemon's own image,
	// so the daemon has to know which that is — the Engine is asked,
	// rather than anything being configured. Off entirely when the
	// daemon is a host process, where there is no such container.
	host := hostexec.NewRunner(docker, bootstrap.OwnImage(ctx, docker, cfg), cfg.InContainer)

	// What lets the machine module read the box's own numbers. Where
	// those are depends on the same fact: a container's /proc/stat is
	// the machine's, and its /proc/net is its own network namespace, so
	// the interfaces are read through the machine's procfs the daemon's
	// container is given. See machine.NewReader and install.sh.
	box := machine.NewReader(cfg.DataDir, cfg.InContainer)

	// What a build pushes with, when what it built has to run on
	// another machine. Generated on first start and kept, because the
	// registry checks it against what a build sends.
	builderToken, _, err := loadOrCreateSecret(cfg.DataDir, builderTokenFileName)
	if err != nil {
		return err
	}

	srv := server.New(db, docker, server.Options{
		WebhookToken:  cfg.Token,
		BuilderToken:  builderToken,
		Builder:       builder,
		LocalRegistry: localRegistry,
		Frontend:      bootstrap.FrontendAddress(cfg),
		DataDir:       cfg.DataDir,
		SetupToken:    setupToken,
		Host:          host,
		Machine:       box,
		Version:       version,
		// What this instance's own two containers were started from.
		// Read back off the running container rather than derived: an
		// operator is free to point either at a mirror, and string
		// surgery on a registry path is how an instance updates itself
		// to an image that does not exist.
		DaemonImage: bootstrap.OwnImage(ctx, docker, cfg),
		WebImage:    cfg.WebImage,
	})
	// A daemon that comes up on the version a running record was moving
	// to **is** that record finishing: the container that started it has
	// gone, and it went before this process existed.
	srv.Updates.Settle()

	// An install upgrading from the release where the domain and contact
	// address were required environment variables keeps them, once.
	if err := srv.Settings.SeedFromEnv(ctx, config.SeedSettings()); err != nil {
		return fmt.Errorf("carry the old environment into settings: %w", err)
	}

	if err := bootstrap.WriteRegistryTokenCert(cfg, registryCert); err != nil {
		return fmt.Errorf("write registry token certificate: %w", err)
	}

	// applyInfrastructure is run now with whatever is configured, and
	// again whenever the operator changes it — adding a domain has to
	// bring the registry up without a restart.
	apply := func(ctx context.Context, values settings.Values) error {
		return applyInfrastructure(ctx, cfg, docker, values, registryCert)
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
	// The same correction for the databases. Their containers come back
	// on their own — Docker's restart policy is unless-stopped — but
	// the rows still describe the world from before the reboot.
	if err := datastore.Reconcile(ctx, srv.Datastores.Repo(), docker); err != nil {
		return fmt.Errorf("reconcile datastores: %w", err)
	}
	// And for the object storage this instance runs. A linked one has
	// no container and is skipped.
	if err := objectstore.Reconcile(ctx, srv.ObjectStores.Repo(), docker); err != nil {
		return fmt.Errorf("reconcile object stores: %w", err)
	}

	srv.SetRegistrySigningKey(registrySigningKey, registryCertDER)

	// What every container is using, sampled on a timer. It runs here
	// rather than inside the server because a server is a request
	// handler — a test builds one and must not thereby start polling
	// Docker every thirty seconds.
	go metrics.NewCollector(db, docker, srv.MetricSources()...).Run(ctx)

	// And what the box under them is doing, on its own ticker: those
	// readings are four files rather than an Engine call each, and a
	// wedged Engine must not be why the machine's own chart has a hole
	// in it.
	go machine.NewCollector(db, box).Run(ctx)

	// Every name this instance serves, written where this machine's
	// Traefik reads it. A worker runs no proxy: what arrives here is
	// routed to whichever machine runs the app, over the mesh, by
	// container name.
	//
	// It runs here for the reason the collectors do, and it is woken as
	// well as ticked — a deploy that swaps a container leaves the file
	// naming the one that has gone, which is a 502 until it is
	// rewritten.
	routes := &app.RouteWriter{
		Apps:    srv.Apps,
		DataDir: cfg.DataDir,
		TLS: func(ctx context.Context) bool {
			values, err := srv.Settings.Load(ctx)
			return err == nil && values.HasTLS()
		},
	}
	srv.Apps.SetRoutesChanged(routes.Wake)
	go routes.Run(ctx)

	// And the one thing that changes how many containers exist without
	// anybody asking. Here rather than in server.New for the reason the
	// collectors are: a server is a request handler, and a test that
	// builds one must not thereby start scaling apps.
	go (&app.Autoscaler{Apps: srv.Apps, Metrics: srv.Metrics}).Run(ctx)

	// And the one that replaces this process. Same reason it is here:
	// a test that builds a server must not thereby start replacing
	// containers.
	go (&update.Scheduler{Updates: srv.Updates, Settings: srv.Settings}).Run(ctx)

	go purgeExpiredSessions(ctx, srv.Users)

	needsSetup, err := srv.Setup.Needed(ctx)
	if err != nil {
		return fmt.Errorf("check whether this instance is set up: %w", err)
	}
	if needsSetup {
		// The token is what keeps whoever reaches the port first from
		// claiming the machine, so the one thing worth logging is where
		// to read it. Never the value: the daemon's logs are not a
		// secret store.
		if setupToken.Required() {
			log.Printf("this instance has no account yet: open it and create one. "+
				"Creating it needs the setup token in %s.", setupToken.Path)
		} else {
			log.Printf("this instance has no account yet, and no setup token was written: " +
				"anyone who can reach this port can claim it.")
		}
	}

	log.Printf("cubeshipd listening on %s", listenAddr)
	if !current.HasDomain() {
		log.Printf("no domain configured yet: the registry is running but only reachable from this host, and apps are served over plain HTTP")
	}
	return http.ListenAndServe(listenAddr, srv.Router())
}
