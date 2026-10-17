package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/backup"
	"cubeship/internal/certificates"
	"cubeship/internal/datastore"
	"cubeship/internal/machine"
	"cubeship/internal/metrics"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/config"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/platform/regauth"
	"cubeship/internal/platform/selfdial"
	"cubeship/internal/server"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/shell"
	"cubeship/internal/templateinstall"
	"cubeship/internal/update"
	"cubeship/internal/user"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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

	var ownNames []string
	if values.HasDomain() {
		ownNames = []string{apiHost, registryHost}
	}
	if err := bootstrap.Ensure(ctx, docker,
		bootstrap.TraefikContainerOpts(cfg, values.HasTLS(), values.Get(settings.ACMEEmail), ownNames)); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}

	// The dashboard. Its own container, which the daemon proxies to for
	// everything that is not /api — so the instance is one address and
	// a session cookie is same-origin for both halves.
	//
	// A failure here does not stop the daemon: the API, the CLI and
	// every running app are unaffected by a dashboard that is down, and
	// refusing to start over it would turn a cosmetic problem into an
	// outage. internal/dashboard says so at the address instead.
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

// purgeAuditEvents drops audit events past audit.Retention, once at start
// and then every six hours.
func purgeAuditEvents(ctx context.Context, events *audit.Service) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		n, err := events.Purge(ctx)
		if err != nil {
			log.Printf("could not purge audit events: %v", err)
		} else if n > 0 {
			log.Printf("purged %d audit event(s) past %s", n, audit.Retention)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
	// Registered under the address the *Engine* will use, because that
	// is the host a pull's credentials are looked up by. The daemon's
	// own address for the registry is a different string and is not
	// what any image reference carries.
	pullRegistry := bootstrap.PullRegistryAddress()
	docker.SetRegistryTokenSigner(pullRegistry, func(repository string) (string, error) {
		return regauth.IssueToken(registrySigningKey, registryCertDER, regauth.TokenIssuer, regauth.TokenService, "cubeshipd",
			[]regauth.AccessEntry{{Type: "repository", Name: repository, Actions: []string{"pull"}}})
	})

	ctx := context.Background()

	if err := bootstrap.PrepareNetworks(ctx, docker, cfg.InContainer); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}
	if cfg.InContainer {
		if err := docker.ConfigureProbes(ctx, bootstrap.DaemonContainerName); err != nil {
			return err
		}
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
	// A root shell belongs to a connection, and every connection the
	// last run of this daemon held is gone.
	host.Sweep(ctx)

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

	catalog := templateinstall.NewHTTPCatalog(cmp.Or(cfg.CatalogURL, templateinstall.DefaultCatalogURL),
		httpx.APIPrefix+"/template-icons/")
	srv := server.New(db, docker, server.Options{
		// An instance that was pointed at somebody else's Postgres has
		// no container here to dump, so it cannot back itself up — and
		// says so rather than writing an archive with a hole in it.
		OwnDatabase:   cfg.DatabaseURL == "",
		WebhookToken:  cfg.Token,
		BuilderToken:  builderToken,
		Builder:       builder,
		LocalRegistry: localRegistry,
		PullRegistry:  pullRegistry,
		Frontend:      bootstrap.FrontendAddress(cfg),
		DataDir:       cfg.DataDir,
		SetupToken:    setupToken,
		Host:          host,
		Terminals:     shell.OnThisMachine(host),
		Machine:       box,
		Version:       version,
		Catalog:       catalog,
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

	// The catalog may be an app on this very instance — cubeship.dev is —
	// and a request for a name that points back here hangs on a host that
	// does not hairpin its own NAT. What reaches this instance from
	// outside is its public address and whatever its own domain resolves
	// to; a name resolving to either goes to Traefik over the network.
	catalog.HTTP = selfdial.Client(&selfdial.Dialer{
		Self: func(ctx context.Context) []string {
			values, err := srv.Settings.Load(ctx)
			if err != nil {
				return nil
			}
			var own []string
			if ip := srv.Settings.PublicIP(ctx, values, ""); ip != "" {
				own = append(own, ip)
			}
			if domain := values.Get(settings.Domain); domain != "" {
				if found, err := net.DefaultResolver.LookupHost(ctx, domain); err == nil {
					own = append(own, found...)
				}
			}
			return own
		},
		Proxy: bootstrap.TraefikContainerName,
	}, catalog.HTTP.Timeout)

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
	if err := app.SettleInterrupted(ctx, srv.Apps.Repo()); err != nil {
		return fmt.Errorf("settle interrupted deploys: %w", err)
	}
	if err := backup.SettleInterrupted(ctx, srv.Backups.Repo()); err != nil {
		return fmt.Errorf("settle interrupted backups: %w", err)
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
	stores, err := srv.ObjectStores.Repo().List(ctx)
	if err != nil {
		return fmt.Errorf("list managed stores for network migration: %w", err)
	}
	for _, store := range stores {
		if store.Kind != objectstore.KindManaged || store.ContainerID == "" {
			continue
		}
		if err := docker.ReconcileNetworks(ctx, store.ContainerID, []string{dockerx.ManagementNetwork}, nil); err != nil && !errors.Is(err, dockerx.ErrContainerNotFound) {
			return fmt.Errorf("connect managed store to control network: %w", err)
		}
	}
	// An install this daemon's previous run was applying has nobody
	// applying it now, so what it created is undone.
	if err := srv.Templates.Recover(ctx); err != nil {
		log.Printf("template installs: could not undo the ones left running: %v", err)
	}

	// The host's stanza, rendered from whatever is exposed now. Every
	// expose rewrites it too; this is for what changed while the daemon
	// was not running, and for an instance upgraded from one that kept
	// no such lines.
	if err := srv.Firewall.SyncPublished(ctx); err != nil {
		log.Printf("firewall: could not bring the host stanza in line with what is exposed: %v", err)
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

	// Traefik asks Let's Encrypt for a certificate when its
	// configuration changes and at no other moment, so a name whose
	// first attempt failed would otherwise never get a second one.
	go (&certificates.Retrier{Certs: srv.Certs, DataDir: cfg.DataDir}).Run(ctx)

	// And the backups nobody asked for. Here for the reason every other
	// loop is: a server is a request handler, and a test that builds one
	// must not thereby start dumping databases.
	go (&backup.Scheduler{Backups: srv.Backups}).Run(ctx)

	go purgeExpiredSessions(ctx, srv.Users)
	go purgeAuditEvents(ctx, srv.Audit)

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
	return (&http.Server{
		Addr: listenAddr, Handler: srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}).ListenAndServe()
}
