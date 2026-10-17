package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	// The timezone database, in the binary.
	//
	// **The image is Alpine and Alpine ships none**, so
	// time.LoadLocation found nothing and every zone name was refused:
	// an instance told to update itself at 03:00 in America/Bahia was
	// told that is not a timezone this machine knows. Which it is.
	//
	// Embedded rather than `apk add tzdata`, for the reason the fonts
	// are vendored: a binary that needs something from the image
	// underneath it is one that breaks the day somebody builds it on a
	// smaller base, and finding that out costs somebody an evening.
	// About 450KB, once.
	_ "time/tzdata"

	"cubeship/internal/machine"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/settings"
	"cubeship/internal/update"
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
	probe := flag.String("probe", "", "run one isolated readiness probe and exit")
	probeAddress := flag.String("probe-address", "", "readiness target address")
	probePath := flag.String("probe-path", "", "readiness HTTP path")
	showVersion := flag.Bool("version", false, "print version and exit")
	// The one mode that is not a daemon at all: a throwaway container
	// started by the daemon it is about to replace. See replaceDaemon.
	replace := flag.String("replace", "", "replace this container with a new image and exit")
	replaceImage := flag.String("replace-image", "", "the image to replace it with")
	flag.Parse()
	if *probe != "" {
		if err := dockerx.RunProbe(context.Background(), *probe, *probeAddress, *probePath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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
	spec, err = bootstrap.DaemonReplacement(spec)
	if err != nil {
		store.Finish(run, err)
		return err
	}
	if err := docker.EnsureNetwork(ctx, dockerx.ManagementNetwork); err != nil {
		store.Finish(run, err)
		return err
	}

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
	if err := bootstrap.PrepareNetworks(ctx, docker, cfg.InContainer); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}
	if cfg.InContainer {
		if err := docker.ConfigureProbes(ctx, bootstrap.DaemonContainerName); err != nil {
			return err
		}
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
