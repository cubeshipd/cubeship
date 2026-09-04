package server

import (
	"net/http"

	"cubeship/internal/app"
	"cubeship/internal/org"
	"cubeship/internal/project"
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
	return s.BuildMCPServer(caller, user.KeyHashFromContext(r.Context()))
}

// BuildMCPServer registers every module's tools for one already
// authenticated caller. Building a fresh server per request (rather than
// one shared server reused across everyone) is what lets each tool close
// over the caller with no risk of it leaking between them.
//
// It is exported so tests can drive the same server the endpoint serves.
func (s *Server) BuildMCPServer(caller *user.User, keyHash string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "cubeship", Version: mcpVersion}, nil)
	user.NewTools(s.Users, caller, keyHash).Register(srv)
	org.NewTools(s.Orgs, caller).Register(srv)
	project.NewTools(s.Projects, caller).Register(srv)
	app.NewTools(s.Apps, caller).Register(srv)
	return srv
}
