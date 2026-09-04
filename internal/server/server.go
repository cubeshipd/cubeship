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
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/registry"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/user"
)

// Server owns the module graph and the mux they are mounted on.
type Server struct {
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
	Apps     *app.Service
	Settings *settings.Service
	Setup    *setup.Service
	Registry *registry.Handler

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
}

// New wires the modules together. The dependency order here is the real
// one: users know nothing of organizations, organizations authorize
// everything below them, and apps sit at the bottom.
func New(db *database.DB, docker app.DockerAPI, opts Options) *Server {
	users := user.NewService(db)
	orgs := org.NewService(db, users)
	projects := project.NewService(db, orgs)
	cfg := settings.NewService(db)
	apps := app.NewService(db, orgs, projects, app.NewOrchestrator(db, docker, cfg), cfg)

	srv := &Server{
		Users:    users,
		Orgs:     orgs,
		Projects: projects,
		Apps:     apps,
		Settings: cfg,
		Setup:    setup.NewService(db, users),
		Registry: registry.NewHandler(users, orgs, apps, cfg, opts.WebhookToken),
		router:   httpx.NewRouter(),
	}
	srv.routes()
	return srv
}

// SetRegistrySigningKey wires the daemon's registry-token signing key
// into the registry module. Must be called before serving.
func (s *Server) SetRegistrySigningKey(key *rsa.PrivateKey) {
	s.Registry.SetSigningKey(key)
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
	// None of these belong in the document: a liveness probe, the
	// document itself, the page that renders it, and an endpoint that
	// speaks JSON-RPC rather than REST.
	s.router.HandleInternalFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Both are unauthenticated so a browser can render the reference
	// without a key. See handleDocs.
	s.router.HandleInternalFunc("GET "+OpenAPIPath, s.handleOpenAPI)
	s.router.HandleInternalFunc("GET "+DocsPath, s.handleDocs)

	// auth is handed to each module so every authenticated route is
	// mounted the same way, and no module invents its own.
	userHandler := user.NewHandler(s.Users)
	auth := userHandler.Middleware

	userHandler.Routes(s.router, auth)
	// Setup is the one surface that cannot require being signed in:
	// before it runs there is nobody to be.
	setup.NewHandler(s.Setup, userHandler.StartSession).Routes(s.router)
	org.NewHandler(s.Orgs).Routes(s.router, auth)
	project.NewHandler(s.Projects).Routes(s.router, auth)
	settings.NewHandler(s.Settings).Routes(s.router, auth)
	app.NewHandler(s.Apps).Routes(s.router, auth)

	// The registry's own two endpoints authenticate differently (Basic
	// auth, and a shared webhook secret), so they mount unwrapped.
	s.Registry.Routes(s.router)

	s.router.HandleInternal("POST /mcp", auth(s.mcpHandler()))
}
