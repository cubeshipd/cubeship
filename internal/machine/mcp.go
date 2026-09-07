package machine

import (
	"context"

	"cubeship/internal/metrics"
	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// One tool, and it only reads.
//
// The line internal/firewall draws by having none at all is about
// *acting* on the machine — an agent that closes 443 takes the instance
// off the internet. Nothing here acts: this is the answer to "is the
// box out of memory", which is the question an agent asked to work out
// why a deploy is slow would otherwise guess at.
type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "instance_metrics",
		Description: "Read what the machine this Cubeship instance runs on has been doing: how much of its CPU is busy (100 is every core, unlike an app's series where 100 is one core), how much of its memory is spoken for, how full the disk everything is kept on is, and how fast bytes are moving over its own interfaces. Samples are 30 seconds apart and a day is kept. A measurement this daemon cannot take comes back under `unavailable` with the reason, rather than as a number that is wrong.",
	}, t.metrics)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "instance_containers",
		Description: "List what every container on this instance is using right now — apps, databases and managed object stores together — heaviest CPU first. Each carries the name that identifies it: an app's full project/environment/name reference, a database's or a store's name. The CPU convention here is the container one: 100 is one core, unlike instance_metrics, where 100 is the whole machine. This is what answers \"what is eating this box\" without opening every app in turn.",
	}, t.containers)
}

type metricsInput struct {
	Window string `json:"window,omitempty" jsonschema:"how much of the past to cover: 1h, 6h or 24h. Defaults to 1h"`
}

func (t *Tools) metrics(ctx context.Context, _ *mcp.CallToolRequest, in metricsInput) (*mcp.CallToolResult, Series, error) {
	series, err := t.svc.Series(ctx, t.caller, in.Window)
	if err != nil {
		return nil, Series{}, err
	}
	return nil, series, nil
}

func (t *Tools) containers(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []metrics.Usage, error) {
	usage, err := t.svc.Containers(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	return nil, usage, nil
}
