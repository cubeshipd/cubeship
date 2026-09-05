// Package server assembles the domain modules into the daemon's two
// surfaces: the HTTP API and the MCP endpoint.
//
// It is the only place that knows every module exists. Modules never
// reach for each other's transports — they depend on each other's
// services, and it is this package that mounts them.
package server

import (
	"crypto/rsa"
	"net/http"

	"cubeship/internal/app"
	"cubeship/internal/dns"
	"cubeship/internal/extregistry"
	"cubeship/internal/github"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/registry"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/user"
	"cubeship/internal/web"
)

// Server owns the module graph and the mux they are mounted on.
type Server struct {
	Users      *user.Service
	Projects   *project.Service
	Apps       *app.Service
	Settings   *settings.Service
	Setup      *setup.Service
	Registries *extregistry.Service
	DNS        *dns.Service
	GitHub     *github.Service
	Registry   *registry.Handler

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

	// Builder turns a repository into an image. A server without one
	// serves everything except a deploy of an app that builds, which
	// refuses rather than panicking — which is what most tests want.
	Builder app.ImageBuilder

	// LocalRegistry is where the daemon pulls an app's own image from.
	// It depends on whether the daemon is a container or a host process,
	// which only the daemon knows.
	LocalRegistry string

	// Frontend is where the dashboard's server answers. The daemon is
	// the only thing in front of it, so this is the address of a
	// container on the shared network — or, on a developer's machine,
	// of `make web-dev`.
	Frontend string
}

// New wires the modules together. The dependency order here is the real
// one: users authorize everything, and apps sit at the bottom.
func New(db *database.DB, docker app.DockerAPI, opts Options) *Server {
	users := user.NewService(db)
	projects := project.NewService(db)
	cfg := settings.NewService(db)
	registries := extregistry.NewService(db)
	dnsProviders := dns.NewService(db)
	gh := github.NewService(db, cfg)
	apps := app.NewService(db, projects,
		app.NewOrchestrator(db, docker, cfg, registries, opts.Builder, gh, opts.LocalRegistry), cfg)

	// Deleting a project or an environment takes the apps inside it with
	// it, and only this module knows how to stop a container. The
	// dependency runs downward everywhere else, so it is handed back up
	// here — the one place that knows every module exists.
	projects.SetAppTeardown(apps)

	srv := &Server{
		Users:      users,
		Projects:   projects,
		Apps:       apps,
		Settings:   cfg,
		Setup:      setup.NewService(db, users),
		Registries: registries,
		DNS:        dnsProviders,
		GitHub:     gh,
		Registry:   registry.NewHandler(users, apps, cfg, opts.WebhookToken, opts.LocalRegistry),
		frontend:   opts.Frontend,
		router:     httpx.NewRouter(),
	}
	// Garbage collection runs a command inside the registry container,
	// which needs the Engine rather than the deploy interface. A fake in
	// a test is not one, and the endpoint refuses rather than pretending.
	if m, ok := docker.(registry.Maintainer); ok {
		srv.Registry.SetMaintainer(m)
	}

	srv.routes()
	return srv
}

// WaitForGitHubDeploys blocks until every deploy a GitHub webhook
// started has finished. For tests.
func (s *Server) WaitForGitHubDeploys() { s.githubHandler.WaitForDeploys() }

// SetRegistrySigningKey wires the daemon's registry-token signing key
// into the registry module. Must be called before serving.
func (s *Server) SetRegistrySigningKey(key *rsa.PrivateKey, certDER []byte) {
	s.Registry.SetSigningKey(key, certDER)
}

// Router returns the daemon's HTTP handler.
func (s *Server) Router() http.Handler { return s.router }

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
	auth := userHandler.Middleware

	userHandler.Routes(s.router, auth)
	// Setup is the one surface that cannot require being signed in:
	// before it runs there is nobody to be.
	setup.NewHandler(s.Setup, userHandler.StartSession).Routes(s.router)
	project.NewHandler(s.Projects).Routes(s.router, auth)
	settings.NewHandler(s.Settings).Routes(s.router, auth)
	extregistry.NewHandler(s.Registries).Routes(s.router, auth)
	dns.NewHandler(s.DNS).Routes(s.router, auth)
	s.githubHandler = github.NewHandler(s.GitHub, s.Apps)
	s.githubHandler.Routes(s.router, auth)
	app.NewHandler(s.Apps).Routes(s.router, auth)

	// The registry's own two endpoints authenticate differently (Basic
	// auth, and a shared webhook secret), so they mount unwrapped. So
	// does GitHub's, which is signed rather than bearing a key.
	s.Registry.Routes(s.router)
	s.Registry.CatalogueRoutes(s.router, auth)
	s.githubHandler.WebhookRoutes(s.router)

	s.router.HandleRoot("POST /mcp", auth(s.mcpHandler()))

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
	s.router.HandleRoot("/", web.Handler(s.frontend))
}
