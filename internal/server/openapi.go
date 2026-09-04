package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/org"
	"cubeship/internal/platform/openapi"
	"cubeship/internal/project"
	"cubeship/internal/registry"
	"cubeship/internal/user"
)

// APIVersion is the version reported in the OpenAPI document.
const APIVersion = "0.1.0"

// BearerScheme is the security scheme almost every endpoint uses: the
// caller's API key as a bearer token.
const BearerScheme = "apiKey"

// OpenAPIPath and DocsPath are where the machine-readable document and
// the human-readable reference are served.
const (
	OpenAPIPath = "/openapi.json"
	DocsPath    = "/docs"
)

// OpenAPI assembles the whole document from each module's own
// declaration. Nothing here describes an endpoint — a module that serves
// a route is the module that documents it.
func (s *Server) OpenAPI() openapi.Document {
	merged := openapi.Merge(
		user.NewHandler(s.Users).OpenAPI(),
		org.NewHandler(s.Orgs).OpenAPI(),
		project.NewHandler(s.Projects).OpenAPI(),
		app.NewHandler(s.Apps).OpenAPI(),
		s.Registry.OpenAPI(),
		serverOwnedSpec(),
	)

	return openapi.Document{
		OpenAPI: "3.1.0",
		Info: openapi.Info{
			Title:   "Cubeship",
			Version: APIVersion,
			Description: "The HTTP API of a Cubeship daemon.\n\n" +
				"Authenticate with your API key as a bearer token: `Authorization: Bearer <key>`. " +
				"The same key authenticates the MCP endpoint and `docker login` against the embedded registry.\n\n" +
				"**On 403 versus 404:** a resource you cannot see returns 404, identical to one that does not exist, " +
				"so a valid API key cannot be used to discover other tenants' organizations or app names. " +
				"403 is reserved for a caller who does belong to the organization and merely lacks the role.",
		},
		Servers: s.servers(""),
		Tags:    merged.Tags,
		Paths:   merged.Paths,
		Components: openapi.Components{
			Schemas: merged.Schemas,
			SecuritySchemes: map[string]*openapi.SecurityScheme{
				BearerScheme: {
					Type:        "http",
					Scheme:      "bearer",
					Description: "Your Cubeship API key. The super-admin's first key is written to $CUBESHIP_DATA_DIR/admin-api-key on first boot.",
				},
				registry.BasicAuthScheme: {
					Type:        "http",
					Scheme:      "basic",
					Description: "Your username and API key, as Docker sends them to the registry's token realm.",
				},
				registry.WebhookTokenScheme: {
					Type:        "http",
					Scheme:      "bearer",
					Description: "The daemon's own system token, sent by the registry container. Not a user credential.",
				},
			},
		},
		Security: []openapi.SecurityRequirement{{BearerScheme: {}}},
	}
}

// serverOwnedSpec covers the handful of routes the server mounts itself
// rather than delegating to a module.
func serverOwnedSpec() openapi.Spec {
	return openapi.Spec{
		Tags: []openapi.Tag{{
			Name:        "Daemon",
			Description: "The daemon's own endpoints: liveness, this document, and the MCP surface.",
		}},
		Paths: map[string]openapi.PathItem{
			"/healthz": {
				"get": {
					OperationID: "healthz",
					Summary:     "Liveness check",
					Description: "Always 200 while the daemon is serving. Needs no credentials, so it is safe for an uptime monitor.",
					Tags:        []string{"Daemon"},
					Security:    openapi.Public(),
					Responses:   openapi.Responses{"200": openapi.Empty("The daemon is up.")},
				},
			},
			OpenAPIPath: {
				"get": {
					OperationID: "openapiDocument",
					Summary:     "This document",
					Description: "Served without authentication so the reference page can load it from a browser. It describes shapes, never data.",
					Tags:        []string{"Daemon"},
					Security:    openapi.Public(),
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("The OpenAPI document.", &openapi.Schema{Type: "object"}),
					},
				},
			},
			DocsPath: {
				"get": {
					OperationID: "apiReference",
					Summary:     "API reference",
					Description: "A browsable reference rendered from this document.",
					Tags:        []string{"Daemon"},
					Security:    openapi.Public(),
					Responses: openapi.Responses{
						"200": {
							Description: "The reference page.",
							Content:     map[string]openapi.MediaType{"text/html": {Schema: openapi.String("")}},
						},
					},
				},
			},
			"/mcp": {
				"post": {
					OperationID: "mcp",
					Summary:     "Model Context Protocol endpoint",
					Description: "Everything the CLI can do, exposed as MCP tools over the streamable-HTTP transport, authorized exactly like the equivalent request above.\n\nThis is JSON-RPC, not REST, so the request and response shapes are the MCP protocol's rather than anything described here — point an MCP client at it instead of calling it by hand. Give the client an API key of its own (POST /users/me/api-keys); rotating your terminal's key then never breaks it.",
					Tags:        []string{"Daemon"},
					RequestBody: &openapi.RequestBody{
						Required:    true,
						Description: "A JSON-RPC message, as defined by the Model Context Protocol.",
						Content:     openapi.JSON(&openapi.Schema{Type: "object"}),
					},
					Responses: openapi.Responses{
						"200": openapi.JSONResponse("A JSON-RPC response, or an SSE stream.", &openapi.Schema{Type: "object"}),
						"401": openapi.Unauthorized,
					},
				},
			},
		},
	}
}

// servers returns the base URLs the document offers, most specific
// first. origin is the address the document is being fetched from, when
// there is one.
//
// Listing the request's own origin first is what makes the reference
// page's "try it" actually work: whatever address you opened the docs on
// — api.example.com through Traefik, or 127.0.0.1:9000 over an SSH
// tunnel — is the address the requests go to, with no field to fill in.
func (s *Server) servers(origin string) []openapi.Server {
	var out []openapi.Server
	if origin != "" {
		out = append(out, openapi.Server{URL: origin, Description: "This daemon, at the address you are reading these docs from."})
	}
	if canonical := s.canonicalURL(); canonical != "" && canonical != origin {
		out = append(out, openapi.Server{URL: canonical, Description: "This daemon's public address, through Traefik."})
	}
	return out
}

// canonicalURL is the daemon's configured public address, or empty when
// it was never configured (as in tests).
func (s *Server) canonicalURL() string {
	if s.apiHost == "" {
		return ""
	}
	return "https://" + s.apiHost
}

// handleOpenAPI serves the document. It is generated on each request
// rather than cached: it costs microseconds, a cached copy is one more
// thing that can go stale, and the server list depends on the request
// anyway.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	doc := s.OpenAPI()
	doc.Servers = s.servers(requestOrigin(r))

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(doc)
}

// requestOrigin reconstructs the scheme://host a request arrived on.
//
// The X-Forwarded-* headers are honoured because the daemon always sits
// behind its own Traefik, which sets them; without that, a document
// fetched through the proxy would advertise plain http. Trusting them is
// safe here in a way it would not be for auth or redirects: the only
// thing this value does is fill in a base URL for a page the browser
// already loaded from that same address.
func requestOrigin(r *http.Request) string {
	if r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host

	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		// A chained proxy appends, giving "https, http" — the first entry
		// is the one the client actually spoke.
		scheme = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		host = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return scheme + "://" + host
}
