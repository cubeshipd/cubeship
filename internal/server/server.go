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
	"cubeship/internal/user"
)

// Server owns the module graph and the mux they are mounted on.
type Server struct {
	Users    *user.Service
	Orgs     *org.Service
	Projects *project.Service
	Apps     *app.Service
	Registry *registry.Handler

	apiHost string
	router  *httpx.Router
}

// Options are what the daemon has to supply that the modules cannot
// derive for themselves.
type Options struct {
	// WebhookToken is the shared secret on the registry's push
	// notifications. Not anyone's API key.
	WebhookToken string
	// RegistryHost is the public registry name (registry.<domain>) that
	// app image paths are built from.
	RegistryHost string
	// APIHost is the daemon's own public name (api.<domain>). It appears
	// in the OpenAPI document as the canonical server, so the reference
	// page targets a real address rather than a placeholder.
	APIHost string
}

// New wires the modules together. The dependency order here is the real
// one: users know nothing of organizations, organizations authorize
// everything below them, and apps sit at the bottom.
func New(db *database.DB, docker app.DockerAPI, opts Options) *Server {
	users := user.NewService(db)
	orgs := org.NewService(db, users)
	projects := project.NewService(db, orgs)
	apps := app.NewService(db, orgs, projects, app.NewOrchestrator(db, docker), opts.RegistryHost)

	srv := &Server{
		Users:    users,
		Orgs:     orgs,
		Projects: projects,
		Apps:     apps,
		Registry: registry.NewHandler(users, orgs, apps, opts.WebhookToken, opts.RegistryHost),
		apiHost:  opts.APIHost,
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

func (s *Server) routes() {
	s.router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Both are unauthenticated so a browser can render the reference
	// without a key. See handleDocs.
	s.router.HandleFunc("GET "+OpenAPIPath, s.handleOpenAPI)
	s.router.HandleFunc("GET "+DocsPath, s.handleDocs)

	// auth is handed to each module so every authenticated route is
	// mounted the same way, and no module invents its own.
	userHandler := user.NewHandler(s.Users)
	auth := userHandler.Middleware

	userHandler.Routes(s.router, auth)
	org.NewHandler(s.Orgs).Routes(s.router, auth)
	project.NewHandler(s.Projects).Routes(s.router, auth)
	app.NewHandler(s.Apps).Routes(s.router, auth)

	// The registry's own two endpoints authenticate differently (Basic
	// auth, and a shared webhook secret), so they mount unwrapped.
	s.Registry.Routes(s.router)

	s.router.Handle("POST /mcp", auth(s.mcpHandler()))
}
