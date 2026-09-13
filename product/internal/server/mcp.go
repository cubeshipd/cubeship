package server

import (
	"context"
	"net/http"

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/backup"
	"cubeship/internal/datastore"
	"cubeship/internal/machine"
	"cubeship/internal/node"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/templateinstall"
	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpVersion is reported to MCP clients during their initial handshake.
// It has no relation to the CLI's own version — bump it when a tool's
// behavior changes in a way a client might care about.
const mcpVersion = "0.1.0"

// mcpHandler serves /mcp. Every request builds a fresh, request-scoped
// server (Stateless mode), so a tool call is authorized as whichever
// user's API key the request actually carried, with no server-side
// session state that could outlive or be reused across callers.
//
// It is mounted behind authentication, so serverForRequest always sees an
// authenticated user.
func (s *Server) mcpHandler() http.Handler {
	return mcp.NewStreamableHTTPHandler(s.mcpServerForRequest, &mcp.StreamableHTTPOptions{Stateless: true})
}

func (s *Server) mcpServerForRequest(r *http.Request) *mcp.Server {
	caller := user.FromContext(r.Context())
	if caller == nil {
		// Unreachable in practice — the middleware rejects an
		// unauthenticated request before this is called — but a nil
		// result is exactly how the SDK is documented to produce a 400
		// rather than a panic, so handle it explicitly.
		return nil
	}
	return s.buildMCPServer(caller, user.KeyHashFromContext(r.Context()), audit.ClientIP(r))
}

// BuildMCPServer registers every module's tools for one already
// authenticated caller. Building a fresh server per request (rather than
// one shared server reused across everyone) is what lets each tool close
// over the caller with no risk of it leaking between them.
//
// It is exported so tests can drive the same server the endpoint serves.
func (s *Server) BuildMCPServer(caller *user.User, keyHash string) *mcp.Server {
	return s.buildMCPServer(caller, keyHash, "")
}

func (s *Server) buildMCPServer(caller *user.User, keyHash, ip string) *mcp.Server {
	srv := s.registerMCPTools(caller, keyHash)
	// Removed rather than refused: what a key does not allow is not on
	// the list an agent reads.
	// TestEveryMCPToolIsClassified keeps toolRules the whole list.
	var hidden []string
	for name := range toolRules {
		if !toolAvailable(caller, name) {
			hidden = append(hidden, name)
		}
	}
	srv.RemoveTools(hidden...)
	srv.AddReceivingMiddleware(s.Audit.Middleware(caller, ip, func(tool string) (bool, bool) {
		_, known := toolRules[tool]
		return toolChanges(tool), known && toolAvailable(caller, tool)
	}))
	return srv
}

func (s *Server) registerMCPTools(caller *user.User, keyHash string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "cubeship", Version: mcpVersion}, nil)
	audit.NewTools(s.Audit, caller).Register(srv)
	user.NewTools(s.Users, caller, keyHash).Register(srv)
	project.NewTools(s.Projects, caller).Register(srv)
	app.NewTools(s.Apps, caller).Register(srv)
	datastore.NewTools(s.Datastores, caller).Register(srv)
	objectstore.NewTools(s.ObjectStores, caller).Register(srv)
	templateinstall.NewTools(s.Templates, caller).Register(srv)
	machine.NewTools(s.Machine, caller).Register(srv)
	node.NewTools(s.Nodes, caller).Register(srv)
	backups := backup.NewHandler(s.Backups)
	backups.SetStoreNames(func(id int64) string {
		return s.ObjectStores.NameForID(context.Background(), id)
	})
	backup.NewTools(backups, caller).Register(srv)
	return srv
}
