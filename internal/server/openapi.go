package server

import (
	"encoding/json"
	"net/http"

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
		Servers: []openapi.Server{
			{URL: "https://api.{domain}", Description: "Your daemon, through Traefik."},
		},
		Tags:  merged.Tags,
		Paths: merged.Paths,
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

// handleOpenAPI serves the document. It is generated on each request
// rather than cached: it costs microseconds, and a cached copy is one
// more thing that can go stale after a redeploy.
func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(s.OpenAPI())
}
