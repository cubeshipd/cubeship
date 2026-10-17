package server

import (
	"context"
	"crypto/rsa"
	"io"
	"net/http"

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/backup"
	"cubeship/internal/bitbucket"
	"cubeship/internal/certificates"
	"cubeship/internal/components"
	"cubeship/internal/credential"
	"cubeship/internal/dashboard"
	"cubeship/internal/datastore"
	"cubeship/internal/dns"
	"cubeship/internal/extregistry"
	"cubeship/internal/firewall"
	"cubeship/internal/github"
	"cubeship/internal/machine"
	"cubeship/internal/metrics"
	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/release"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/shell"
	"cubeship/internal/templateinstall"
	"cubeship/internal/update"
	"cubeship/internal/user"
)

// MetricSources are the modules the collector samples on behalf of.
// The daemon builds a Collector from these; a test does not collect at
// all, which is why this is a list rather than a running goroutine.
func (s *Server) MetricSources() []metrics.Source {
	return []metrics.Source{s.Apps, s.Datastores, s.ObjectStores}
}

// WaitForGitHubDeploys blocks until every deploy a GitHub webhook
// started has finished. For tests.
func (s *Server) WaitForGitHubDeploys() {
	s.githubHandler.WaitForDeploys()
	s.Apps.WaitForDeploys()
}

// SetRegistrySigningKey wires the daemon's registry-token signing key
// into the registry module. Must be called before serving.
func (s *Server) SetRegistrySigningKey(key *rsa.PrivateKey, certDER []byte) {
	s.Registry.SetSigningKey(key, certDER)
}

// instanceDatabase is how `backup` reaches this daemon's own Postgres.
//
// Wired here because this is the package that knows both that the
// instance runs one and how to talk to the Engine — the same seam
// `project.AppTeardown` and `credential.Dependant` meet at, and for the
// same reason: the module that needs the answer sits below the one that
// has it.
type instanceDatabase struct {
	docker bootstrap.PostgresDumper
	owned  bool
}

func (i *instanceDatabase) OwnsDatabase() bool { return i.owned }

func (i *instanceDatabase) DumpDatabase(ctx context.Context, w io.Writer) (string, error) {
	return bootstrap.DumpPostgres(ctx, i.docker, w)
}

// Version is the major this instance runs, taken from the image tag
// rather than asked of the server: it is what a restore would have to
// load into, and the image is what decides that.
func (i *instanceDatabase) Version() string { return bootstrap.PostgresMajor() }

// updateDocker is the Engine, if this one can read a container's own
// settings back.
//
// A type assertion rather than a wider parameter, because the only
// caller that cannot satisfy it is a test's stub — and a test that
// builds a server has no business being able to replace the daemon.
func updateDocker(docker app.DockerAPI) update.Docker {
	d, _ := docker.(update.Docker)
	return d
}

// Router returns the daemon's HTTP handler.
//
// **Everything goes through the update guard**, which refuses to change
// anything while this instance is replacing itself. It wraps here
// rather than being wired per module for the reason a lock is worth
// having at all: one place cannot be the place somebody forgot.
func (s *Server) Router() http.Handler {
	return update.Guard(s.Updates)(s.router)
}

// Patterns returns every route pattern registered on the server. The
// OpenAPI parity test uses it to prove the document describes exactly
// what the daemon serves.
func (s *Server) Patterns() []string { return s.router.Patterns() }

// InternalPatterns returns the routes deliberately kept out of the
// OpenAPI document — infrastructure and CLI-only plumbing. The same test
// asserts none of them is documented.
func (s *Server) InternalPatterns() []string { return s.router.InternalPatterns() }

func (s *Server) routes() {
	// None of these belong in the document, and none of them moves
	// under the API prefix: a liveness probe an uptime check is pointed
	// at, the document itself, the page that renders it, and an endpoint
	// that speaks JSON-RPC rather than REST.
	s.router.HandleRootFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Both are unauthenticated so a browser can render the reference
	// without a key. See handleDocs.
	s.router.HandleRootFunc("GET "+OpenAPIPath, s.handleOpenAPI)
	s.router.HandleRootFunc("GET "+DocsPath, s.handleDocs)

	// auth is handed to each module so every authenticated route is
	// mounted the same way, and no module invents its own.
	userHandler := user.NewHandler(s.Users)
	auditHandler := audit.NewHandler(s.Audit)
	// Recorded inside authentication, so every change has somebody behind it.
	auth := func(h http.Handler) http.Handler {
		return userHandler.Middleware(auditHandler.Record(h))
	}
	auditHandler.Routes(s.router, auth)

	userHandler.Routes(s.router, auth)
	// Setup is the one surface that cannot require being signed in:
	// before it runs there is nobody to be.
	setup.NewHandler(s.Setup, userHandler.StartSession).Routes(s.router)
	project.NewHandler(s.Projects).Routes(s.router, auth)
	settings.NewHandler(s.Settings).Routes(s.router, auth)
	certificates.NewHandler(s.Certs).Routes(s.router, auth)
	backups := backup.NewHandler(s.Backups)
	// What a store is called, so a listing of backups can say where one
	// is without the dashboard joining two lists itself. Wired here for
	// the reason every seam between two modules is: `server` is the one
	// package that knows both exist.
	backups.SetStoreNames(func(id int64) string {
		return s.ObjectStores.NameForID(context.Background(), id)
	})
	backups.Routes(s.router, auth)
	firewall.NewHandler(s.Firewall).Routes(s.router, auth)
	credential.NewHandler(s.Credentials).Routes(s.router, auth)
	extregistry.NewHandler(s.Registries).Routes(s.router, auth)
	dns.NewHandler(s.DNS).Routes(s.router, auth)
	s.githubHandler = github.NewHandler(s.GitHub, s.Apps)
	s.githubHandler.Routes(s.router, auth)
	bitbucket.NewHandler(s.Bitbucket, s.Apps).Routes(s.router, auth)
	app.NewHandler(s.Apps).Routes(s.router, auth)
	datastore.NewHandler(s.Datastores).Routes(s.router, auth)
	objectstore.NewHandler(s.ObjectStores).Routes(s.router, auth)
	templateinstall.NewHandler(s.Templates).Routes(s.router, auth)
	machine.NewHandler(s.Machine).Routes(s.router, auth)
	components.NewHandler(s.Components).Routes(s.router, auth)
	release.NewHandler(s.Releases).Routes(s.router, auth)
	update.NewHandler(s.Updates).Routes(s.router, auth)
	// Two surfaces on one module: the operator's behind `auth`, and the
	// agent's behind a node's own credential, which node.Routes wires
	// itself. A worker never reaches anything that takes a caller.
	nodes := node.NewHandler(s.Nodes)
	nodes.Routes(s.router, auth)
	// Beside the machines rather than inside them: a session is opened
	// by a person behind `auth`, and claimed by a worker behind the
	// node's own credential.
	shell.NewHandler(s.Shell, s.Audit, nodes.Agent).Routes(s.router, auth)

	// The registry's own two endpoints authenticate differently (Basic
	// auth, and a shared webhook secret), so they mount unwrapped. So
	// does GitHub's, which is signed rather than bearing a key.
	s.Registry.Routes(s.router)
	s.Registry.CatalogueRoutes(s.router, auth)
	s.githubHandler.WebhookRoutes(s.router)
	bitbucket.NewHandler(s.Bitbucket, s.Apps).WebhookRoutes(s.router)

	// Authenticated only: MCP is one POST for every tool, so the key
	// policy and the log both work per tool, inside it.
	s.router.HandleRoot("POST /mcp", userHandler.Middleware(s.mcpHandler()))

	// A path under the prefix that matches no route is a wrong API call,
	// not a dashboard route. Without this it would fall through to "GET
	// /" below and answer 200 with HTML, which a client reads as a
	// malformed response rather than as the 404 it is.
	s.router.HandleRootFunc(httpx.APIPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no such endpoint", http.StatusNotFound)
	})

	// The dashboard takes the whole root: it is the fallback, matching
	// only what nothing above it claimed. It is registered for every
	// method rather than for GET, because "GET /" and "/api/" are
	// ambiguous to the mux — one matches fewer methods, the other a
	// narrower path — and it refuses to choose. The handler answers 405
	// to anything but a read.
	s.router.HandleRoot("/", dashboard.Handler(s.frontend))
}

// exposedPorts is firewall.Exposed: every host port a datastore, a
// managed object store or an app's TCP port is published on, read from
// their rows.
type exposedPorts struct {
	datastores   *datastore.Repository
	objectStores *objectstore.Repository
	apps         *app.Repository
}

func (e exposedPorts) ExposedPorts(ctx context.Context) ([]int, error) {
	used, err := e.datastores.UsedPorts(ctx)
	if err != nil {
		return nil, err
	}
	ports := make([]int, 0, len(used))
	for port := range used {
		ports = append(ports, port)
	}
	stores, err := e.objectStores.ExposedManagedPorts(ctx)
	if err != nil {
		return nil, err
	}
	ports = append(ports, stores...)
	tcp, err := e.apps.UsedTCPPorts(ctx)
	if err != nil {
		return nil, err
	}
	for port := range tcp {
		ports = append(ports, port)
	}
	return ports, nil
}

// ports narrows the Engine to the one question the firewall asks of it.
// A fake in a test is not a Docker client, and the firewall reports no
// published ports rather than refusing to answer at all.
func ports(docker app.DockerAPI) firewall.Ports {
	if p, ok := docker.(firewall.Ports); ok {
		return p
	}
	return nil
}
