package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_audit_events",
		Description: "Read the audit log, newest first: who changed what, through the dashboard, the API or MCP, and every refused attempt. Filter by user, via (dashboard, api, mcp), outcome (ok, refused, failed) or target (e.g. an app's reference). Pass the returned next as before for older events. Requires the admin role.",
	}, t.list)
}

type listInput struct {
	User    string `json:"user,omitempty" jsonschema:"only this username"`
	Via     string `json:"via,omitempty" jsonschema:"dashboard, api or mcp"`
	Outcome string `json:"outcome,omitempty" jsonschema:"ok, refused or failed"`
	Target  string `json:"target,omitempty" jsonschema:"only events whose target contains this"`
	Before  int64  `json:"before,omitempty" jsonschema:"an event id; only older events"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many, up to 500; default 100"`
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, Page, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)
	events, err := t.svc.List(ctx, t.caller, Filter{
		Username: in.User, Via: Via(in.Via), Outcome: Outcome(in.Outcome),
		Target: in.Target, Before: in.Before, Limit: limit,
	})
	if err != nil {
		return nil, Page{}, err
	}
	return nil, toPage(events, limit), nil
}

// Classify says, for one tool, whether calling it changes anything and
// whether this caller has it at all.
type Classify func(tool string) (change, available bool)

// Middleware records every tool call that changes something, and every
// call to a tool this caller does not have.
func (s *Service) Middleware(caller *user.User, ip string, classify Classify) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if method != "tools/call" || !ok || call.Params == nil {
				return next(ctx, method, req)
			}
			change, available := classify(call.Params.Name)
			res, err := next(ctx, method, req)
			if available && !change {
				return res, err
			}

			e := From(caller, ViaMCP, ip)
			e.Action = "mcp " + call.Params.Name
			e.Target = targetOf(call.Params.Arguments)
			e.Outcome = OutcomeOK
			result, _ := res.(*mcp.CallToolResult)
			switch {
			case !available:
				e.Outcome, e.Detail = OutcomeRefused, "not available to this API key"
			case err != nil:
				e.Outcome, e.Detail = OutcomeFailed, err.Error()
			case result != nil && result.IsError:
				e.Outcome, e.Detail = OutcomeFailed, errorText(result)
				if strings.HasPrefix(e.Detail, "forbidden") {
					e.Outcome = OutcomeRefused
				}
			}
			s.Record(ctx, e)
			return res, err
		}
	}
}

// naming are the arguments kept from a tool call: the ones that say what
// it was about. A list of what to keep rather than of what to drop, so
// an argument added later that carries a value somebody set is not
// written down by default.
var naming = map[string]bool{
	"app": true, "reference": true, "project": true, "environment": true, "name": true,
	"slug": true, "datastore": true, "store": true, "bucket": true, "id": true,
	"volume_id": true, "owner": true, "repo": true, "release": true, "tag": true,
	"path": true, "engine": true, "access": true, "server": true,
}

// targetOf keeps the arguments that name something, as `key=value`.
func targetOf(raw json.RawMessage) string {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return ""
	}
	var parts []string
	for name, v := range args {
		if !naming[name] {
			continue
		}
		switch v := v.(type) {
		case string:
			if v != "" && len(v) <= 120 {
				parts = append(parts, fmt.Sprintf("%s=%s", name, v))
			}
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%v", name, v))
		}
	}
	sort.Strings(parts)
	out := strings.Join(parts, " ")
	if len(out) > maxDetail {
		out = out[:maxDetail]
	}
	return out
}

func errorText(r *mcp.CallToolResult) string {
	for _, c := range r.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return t.Text
		}
	}
	return ""
}
