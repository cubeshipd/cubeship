package node

import (
	"context"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// One tool, and it only reads.
//
// **Adding a machine mints a credential**, and a credential that passes
// through a model's context is the line `internal/credential` draws by
// having no tools at all. Removing one is the other half of the same
// act and would be a machine dropped out of a cluster by something that
// cannot see the room.
//
// What is left is the useful half: an agent working out why a deploy is
// slow can see how many machines there are, which of them are answering
// and how loaded each is.
type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_servers",
		Description: "List the machines this Cubeship instance is made of: the control plane — the box it was installed on, which holds the database, the dashboard, the registry and the builder — and the workers connected to it. Each carries what it last reported: its cores, memory and disk, the newest load reading, how many containers it is running, and whether it is answering. A server that has never connected is \"pending\"; one that connected and stopped is \"unreachable\", which says nothing about why.",
	}, t.list)
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []Response, error) {
	nodes, err := t.svc.List(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Response, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toResponse(n))
	}
	return nil, out, nil
}
