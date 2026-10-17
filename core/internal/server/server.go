// Package server assembles the domain modules into the daemon's two
// surfaces: the HTTP API and the MCP endpoint.
//
// It is the only place that knows every module exists. Modules never
// reach for each other's transports — they depend on each other's
// services, and it is this package that mounts them.
package server

import (
	"context"
	"fmt"

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/backup"
	"cubeship/internal/bitbucket"
	"cubeship/internal/certificates"
	"cubeship/internal/components"
	"cubeship/internal/credential"
	"cubeship/internal/datastore"
	"cubeship/internal/dns"
	"cubeship/internal/extregistry"
	"cubeship/internal/firewall"
	"cubeship/internal/github"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/metrics"
	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/registry"
	"cubeship/internal/release"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/shell"
	"cubeship/internal/templateinstall"
	"cubeship/internal/update"
	"cubeship/internal/user"
)

// Server owns the module graph and the mux they are mounted on.
type Server struct {
	Users        *user.Service
	Projects     *project.Service
	Apps         *app.Service
	Datastores   *datastore.Service
	ObjectStores *objectstore.Service
	// Templates installs what the catalog lists. It sits above every
	// module an install creates things through.
	Templates *templateinstall.Service
	Backups   *backup.Service
	// Audit is who changed what, recorded at the API's door and MCP's.
	Audit   *audit.Service
	Metrics *metrics.Service
	// Machine is what the box itself is doing, which belongs to no
	// module below: there is one of it, and nothing here configures it.
	Machine    *machine.Service
	Components *components.Service
	// Nodes is the machines this instance is made of. It sits beside
	// user at the bottom: it knows about no other module, and what runs
	// on a node will reach it through the agent's own loop.
	Nodes *node.Service
	// Shell opens a terminal in an app's container or on a machine. It
	// sits above apps and nodes, which each answer where and for whom.
	Shell       *shell.Service
	Credentials *credential.Service
	Settings    *settings.Service
	Releases    *release.Service
	Updates     *update.Service
	Certs       *certificates.Service
	Firewall    *firewall.Service
	Setup       *setup.Service
	Registries  *extregistry.Service
	DNS         *dns.Service
	GitHub      *github.Service
	Bitbucket   *bitbucket.Service
	Registry    *registry.Handler

	// githubHandler is kept so a test can wait for the deploys a
	// webhook set going.
	githubHandler *github.Handler

	// frontend is where page requests are proxied. Empty in a test,
	// where nothing asks for a page.
	frontend string

	router *httpx.Router
}

// Options are what the daemon has to supply that the modules cannot
// derive for themselves.
//
// Everything that follows the instance's domain — the registry host, the
// API host — is deliberately absent: those live in the settings module
// now, because an operator sets the domain from the dashboard after
// installing and the answer has to change without a restart.
type Options struct {
	// WebhookToken is the shared secret on the registry's push
	// notifications. Not anyone's API key.
	WebhookToken string

	// OwnDatabase says this instance runs the Postgres it stores
	// everything in, rather than having been pointed at somebody
	// else's with CUBESHIP_DATABASE_URL. It is what decides whether the
	// instance can back *itself* up: there is no container here to dump
	// through otherwise, and `backup` refuses with that sentence rather
	// than producing an archive with a hole in it.
	OwnDatabase bool

	// BuilderToken is what a build logs in to this instance's own
	// registry with when what it builds runs on another machine. Empty
	// on a server that does not build and push — a test — and then
	// there is no builder login for the registry to accept.
	BuilderToken string

	// Builder turns a repository into an image. A server without one
	// serves everything except a deploy of an app that builds, which
	// refuses rather than panicking — which is what most tests want.
	Builder app.ImageBuilder

	// LocalRegistry is where the daemon reaches the registry's own API —
	// the catalogue, a delete, a garbage-collection pass. It depends on
	// whether the daemon is a container or a host process, which only
	// the daemon knows.
	LocalRegistry string

	// PullRegistry is where the *Engine* pulls an app's image from, and
	// it is a different address for a reason worth reading once:
	// bootstrap.PullRegistryAddress. Using LocalRegistry here is a
	// reference the Engine cannot resolve.
	PullRegistry string

	// Frontend is where the dashboard's server answers. The daemon is
	// the only thing in front of it, so this is the address of a
	// container on the shared network — or, on a developer's machine,
	// of `make dashboard-dev`.
	Frontend string

	// DataDir is where the instance keeps its state, and the one thing
	// outside the database a module reads: Traefik's certificate store
	// lives in it.
	DataDir string

	// Host runs a command on the machine this instance is on, which is
	// what a firewall is made of. Nil in a test and on a daemon that is
	// itself a host process: both would otherwise be editing somebody's
	// real netfilter tables.
	Host firewall.Host

	// Terminals opens shells on this machine: in a container, and on
	// the host itself. Nil in a test, where a session answers that this
	// daemon cannot open one.
	Terminals shell.Local

	// Machine reads what the box itself is doing — its CPU, memory,
	// disk and network. The daemon builds it because where those
	// numbers are depends on whether the daemon is a container: see
	// machine.NewReader. Nil is a reader for a host process, which is
	// what a test is.
	Machine *machine.Reader

	// SetupToken guards claiming an unclaimed instance. The daemon
	// writes it into the data directory on first start and the
	// installer prints it; a zero value asks for none, which is what a
	// test wants and what an instance already claimed has.
	SetupToken setup.Token

	// Version is what this build is, stamped at link time. Empty on a
	// build with nothing stamped on it — `make dev`, and every test —
	// which is an instance with no release to be on and therefore
	// nothing to say changed.
	Version string

	// Catalog is where templates are listed and read from. Nil refuses
	// both, which is what a test's server wants.
	Catalog templateinstall.Catalog

	// DaemonImage and WebImage are what this instance's own two
	// containers are pulled from. Empty on a daemon that is not a
	// container, which cannot update itself and says so.
	//
	// Read back from the running container rather than derived, and
	// passed rather than looked up here, for the same reason
	// CUBESHIP_WEB_IMAGE is told rather than derived: an operator is
	// free to point either at a mirror, and string surgery on a
	// registry path is how an instance ends up updating to an image
	// that does not exist.
	DaemonImage string
	WebImage    string
}

// New wires the modules together. The dependency order here is the real
// one: users authorize everything, and apps sit at the bottom.
func New(db *database.DB, docker app.DockerAPI, opts Options) *Server {
	users := user.NewService(db)
	projects := project.NewService(db, opts.DataDir)
	cfg := settings.NewService(db)
	// What this instance's DNS records should point at cannot be read
	// from inside the daemon's own container — what it finds there is a
	// bridge address, and one of those in an A record is a domain that
	// stops resolving. The host is asked instead, through the same door
	// the firewall uses.
	if opts.Host != nil && opts.Host.Available() {
		cfg.SetHostAddress(settings.RouteAddress(func(ctx context.Context, argv ...string) (string, error) {
			res, err := opts.Host.Run(ctx, argv...)
			if err != nil {
				return "", err
			}
			if !res.OK() {
				return "", fmt.Errorf("%s: exit %d", argv[0], res.Code)
			}
			return res.Output, nil
		}))
	}
	// One store for every secret this instance holds, and two modules
	// that name one rather than keeping their own. See
	// internal/credential: an AWS key reaches Route 53 and ECR both,
	// and it is stored once and pointed at twice.
	creds := credential.NewService(db)
	registries := extregistry.NewService(db, creds)
	dnsProviders := dns.NewService(db, creds, cfg)
	gh := github.NewService(db, cfg)
	bb := bitbucket.NewService(db, cfg)
	// One series service for every module below: an app, a database and
	// the MinIO behind a managed store are the same question about
	// three kinds of container.
	series := metrics.NewService(db)
	apps := app.NewService(db, projects,
		app.NewOrchestrator(db, docker, cfg, registries, opts.Builder, gh, opts.PullRegistry),
		cfg, series)
	apps.SetDataDir(opts.DataDir)

	// Datastores sit above apps: an attachment names one, so this module
	// depends on that one. Nothing below them knows they exist —
	// a datastore belongs to the instance, so deleting a project takes
	// no database with it, and an attachment to a deleted app is
	// removed by the foreign key.
	datastores := datastore.NewService(db, apps,
		datastore.NewProvisioner(db, docker, opts.DataDir), cfg, series)

	// Object storage is the instance's too, and sits beside the
	// databases rather than under them: it depends on credential, for
	// the login an external store authenticates as, and on nothing
	// else. Nothing below it knows it exists.
	objectStores := objectstore.NewService(db, creds, apps,
		objectstore.NewProvisioner(db, docker, opts.DataDir), cfg, series)

	// Installing a template creates through every module above, so it
	// sits on top of them and nothing below knows it exists. No catalog
	// is a test's server: listing and installing refuse, and the rest runs.
	templates := templateinstall.NewService(templateinstall.NewRepository(db),
		projects, apps, datastores, objectStores, user.NewRepository(db), opts.Catalog, opts.Version)

	// The box itself, which sits beside all of them rather than under
	// any: one machine, no configuration, and the only module here that
	// owns nothing but a history.
	reader := opts.Machine
	if reader == nil {
		reader = machine.NewReader(opts.DataDir, false)
	}

	// And the machines beside it. What the cluster's own network is
	// made of is wired here rather than passed to the constructor,
	// because two of the three are things only the daemon has — a
	// Docker that can cluster and a way to reach the host's firewall —
	// and the third is a question for another module.
	nodes := node.NewService(db)
	if engine, ok := docker.(*dockerx.Client); ok {
		nodes.SetStorage(engine)
	}
	// The builder keeps its layers in a store the Engine cannot see, so
	// the storage screen asks it directly.
	if cache, ok := opts.Builder.(node.BuilderCache); ok {
		nodes.SetBuilderCache(cache)
	}
	if engine, ok := docker.(mesh.Engine); ok {
		nodes.SetMesh(engine, opts.Host, func(ctx context.Context) string {
			values, err := cfg.Load(ctx)
			if err != nil {
				return ""
			}
			// The same answer the DNS screens write into a record, and
			// filtered the same way: a private address here would be a
			// swarm the other machines cannot join.
			return cfg.PublicIP(ctx, values, "")
		})
	}

	// Deleting a project or an environment takes the apps inside it with
	// it, and only this module knows how to stop a container. The
	// dependency runs downward everywhere else, so it is handed back up
	// here — the one place that knows every module exists.
	projects.SetAppTeardown(apps)

	// Every container that has to be reachable from another machine
	// joins the cluster's overlay as well as the local bridge: an app,
	// a database, a managed store — the three an attachment addresses
	// by container name. The instance's own infrastructure does not:
	// Postgres, BuildKit and the dashboard are this machine's, and
	// Traefik is each machine's own edge.
	//
	// Existing containers are not touched. One joins the mesh the next
	// time it is created, which is the rule its labels and its
	// environment already follow.
	// What runs where. The module that owns apps sits above the one
	// that owns machines, so it is handed back down here — the same
	// seam project.AppTeardown and credential.Dependant use.
	nodes.SetApps(apps)

	// Updating replaces every machine in the cluster and then this one,
	// so it has to be able to tell the machines. The seam runs the way
	// every other one here does: `node` declares nothing about updates,
	// and is handed to the module that does.
	updates := update.NewService(opts.DataDir, updateDocker(docker), opts.Version,
		bootstrap.DaemonContainerName, opts.DaemonImage,
		bootstrap.FrontendContainerName, opts.WebImage)
	updates.SetCluster(nodes)
	updates.SetBetas(func(ctx context.Context) bool {
		values, err := cfg.Load(ctx)
		return err == nil && values.Get(settings.BetaVersions) == "true"
	})
	// And the other way: what only the machine an app is on can answer
	// — its log today — reaches it through the channel that machine's
	// own poll opens. See app.Remote.
	apps.SetRemote(nodes)
	// Every machine is its own edge, and what its Traefik is started
	// with is this instance's settings — read on every poll rather than
	// captured, because an operator sets the contact address from the
	// dashboard and the machines have to pick it up.
	nodes.SetRegistryHost(func(ctx context.Context) string {
		values, err := cfg.Load(ctx)
		if err != nil {
			return ""
		}
		return settings.RegistryHostFor(values.Get(settings.Domain))
	})

	// What a build authenticates with when its result has to be pushed
	// rather than loaded here. The same secret reaches the registry,
	// which is the other end of that login.
	apps.Orchestrator().SetBuilderLogin(app.BuilderUsername, opts.BuilderToken)

	apps.Orchestrator().SetMeshNetwork(nodes.MeshNetwork)
	datastores.Provisioner().SetMeshNetwork(nodes.MeshNetwork)
	objectStores.Provisioner().SetMeshNetwork(nodes.MeshNetwork)

	// The same three modules the collector samples on behalf of, handed
	// to the series service as well: "what is using this machine" is one
	// question about all of them, and only they can name a subject.
	series.SetSources(apps, datastores, objectStores)

	// What would break if a credential were deleted is known only to
	// the modules using it, so they answer rather than this one
	// reading their rows. Until they are wired, a delete refuses.
	creds.SetDependants(registries, dnsProviders, objectStores)

	// The one thing that travels back down from datastores to apps:
	// what an attached database contributes to a container's
	// environment. app asks for it through an interface rather than
	// importing the module above it — see app.DatastoreVars.
	apps.SetDatastoreVars(datastores)
	// And the same for an attached bucket: S3_ENDPOINT and its parts.
	// A second interface rather than one list, so the env screen can
	// still say whether a variable came from a database or a bucket.
	apps.SetObjectStoreVars(objectStores)

	srv := &Server{
		Users:        users,
		Projects:     projects,
		Apps:         apps,
		Datastores:   datastores,
		ObjectStores: objectStores,
		Metrics:      series,
		Machine:      machine.NewService(db, reader, series),
		Nodes:        nodes,
		Settings:     cfg,
		Templates:    templates,
		Releases:     release.NewService(db, opts.Version),
		Audit:        audit.NewService(db),
		Updates:      updates,
		Certs:        certificates.NewService(cfg, apps, opts.DataDir),
		// Above both of the modules it needs, the way certificates sits
		// above apps and settings: a backup is a database's dump put
		// somewhere that is not this machine, and neither module could
		// own both halves of that sentence.
		Backups: backup.NewService(db, datastores, objectStores, opts.DataDir),
		// A firewall is the host's, so a server with no way to reach the
		// host has one that answers "not available" — which is what a
		// test wants, and what `make dev` is.
		Firewall: firewall.NewService(opts.Host, ports(docker),
			exposedPorts{datastore.NewRepository(db), objectstore.NewRepository(db), app.NewRepository(db)}, opts.DataDir),
		Setup:       setup.NewService(db, users, opts.SetupToken),
		Shell:       shell.NewService(apps, nodes, terminals(opts.Terminals), users),
		Credentials: creds,
		Registries:  registries,
		DNS:         dnsProviders,
		GitHub:      gh,
		Bitbucket:   bb,
		Registry:    registry.NewHandler(users, apps, cfg, opts.WebhookToken, opts.LocalRegistry),
		frontend:    opts.Frontend,
		router:      httpx.NewRouter(),
	}
	// Exposing a database or a store rewrites the host's stanza, which
	// the firewall owns; both modules sit below it and are told.
	datastores.SetPortsChanged(srv.Firewall)
	objectStores.SetPortsChanged(srv.Firewall)
	// An app publishing a TCP port does the same, and asks first which
	// ports the other two already hold.
	apps.SetPortsChanged(srv.Firewall)
	apps.SetHostPorts(exposedPorts{datastore.NewRepository(db), objectstore.NewRepository(db), app.NewRepository(db)})
	// The machines in this cluster pull the images this registry holds,
	// and they authenticate as themselves: their own credential, and
	// pull on the repository they were told to run. See
	// registry.NodeAuth.
	srv.Registry.SetNodeAuth(nodes)
	srv.Registry.SetBuilderToken(opts.BuilderToken)

	// Garbage collection runs a command inside the registry container,
	// which needs the Engine rather than the deploy interface. A fake in
	// a test is not one, and the endpoint refuses rather than pretending.
	if m, ok := docker.(registry.Maintainer); ok {
		srv.Registry.SetMaintainer(m)
	}
	// Why a certificate is missing is in Traefik's log and nowhere else.
	// Without an Engine the report is the same minus those quotations,
	// which is what a test gets.
	if e, ok := docker.(certificates.Engine); ok {
		srv.Certs.SetEngine(e)
	}

	// What it takes to back the instance itself up: an Engine that can
	// exec into this daemon's own Postgres, and the fact that there is
	// one to exec into. A test's Docker is a stub that cannot, and a
	// server pointed at somebody else's database has no container — in
	// either case `backup` refuses with the reason rather than writing
	// an archive with a hole where the database should be.
	srv.Backups.SetVolumes(appVolumes{apps: apps})
	componentEngine, _ := docker.(components.Engine)
	srv.Components = components.NewService(componentEngine, srv.Nodes)
	srv.Machine.SetHosts(srv.Nodes)
	srv.Backups.SetMachines(srv.Nodes)
	if e, ok := docker.(bootstrap.PostgresDumper); ok {
		srv.Backups.SetInstance(&instanceDatabase{docker: e, owned: opts.OwnDatabase})
	}

	srv.routes()
	return srv
}
