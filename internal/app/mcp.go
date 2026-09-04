package app

import (
	"bytes"
	"context"
	"fmt"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/user"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// orgRoleMember is the role every read-and-deploy action needs. Aliased
// here so the handlers and tools don't each spell out the import.
const orgRoleMember = org.RoleMember

// maxMCPLogBytes bounds how much of an app's log get_app_logs returns — a
// large log pasted whole into an LLM's context is mostly waste. Paired
// with a "tail" input so the bytes that do fit are the most recent ones.
const maxMCPLogBytes = 100_000

// mcpDefaultLogTail is deliberately shorter than the HTTP API's: an agent
// reading logs wants the relevant tail, not a transcript.
const mcpDefaultLogTail = "200"

type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_app",
		Description: `Register a new app in a project and get its registry push path. environment defaults to "production" when omitted. Requires member role in the organization.`,
	}, t.create)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_apps",
		Description: "List every app you can see, across every organization you belong to.",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app",
		Description: "Get one app by name: its domain, registry image, status, and which organization/project/environment it lives in.",
	}, t.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy_app",
		Description: `Manually redeploy an app from an image tag already pushed to its registry path (tag defaults to "latest"). Requires member role in the organization.`,
	}, t.deploy)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_app_env",
		Description: "Set an app's own environment variables. Replaces the full set of app-level variables. These are layered on top of (and override) the app's environment's and project's variables. Requires member role in the organization.",
	}, t.setEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_logs",
		Description: "Get an app's recent container log output (stdout and stderr combined).",
	}, t.logs)
}

type createInput struct {
	Org         string `json:"org" jsonschema:"organization slug"`
	Project     string `json:"project" jsonschema:"project slug"`
	Environment string `json:"environment,omitempty" jsonschema:"environment slug (default \"production\")"`
	Name        string `json:"name" jsonschema:"app name: lowercase letters, digits and dashes — becomes part of its registry image path"`
	Domain      string `json:"domain" jsonschema:"domain the app will be served on"`
}

func (t *Tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, Response, error) {
	if in.Domain == "" {
		return nil, Response{}, fmt.Errorf("domain is required")
	}
	created, err := t.svc.Create(ctx, t.caller, in.Org, in.Project, in.Environment, in.Name, in.Domain)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(created), nil
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []Response, error) {
	apps, err := t.svc.List(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	return nil, toResponses(apps), nil
}

type nameInput struct {
	Name string `json:"name" jsonschema:"app name"`
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, Response, error) {
	a, err := t.svc.Resolve(ctx, t.caller, in.Name, orgRoleMember)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(a), nil
}

type deployInput struct {
	Name string `json:"name" jsonschema:"app name"`
	Tag  string `json:"tag,omitempty" jsonschema:"image tag already pushed to the app's registry path (default \"latest\")"`
}

func (t *Tools) deploy(ctx context.Context, _ *mcp.CallToolRequest, in deployInput) (*mcp.CallToolResult, user.ActionResult, error) {
	tag := in.Tag
	if tag == "" {
		tag = "latest"
	}
	a, err := t.svc.Deploy(ctx, t.caller, in.Name, tag)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deployed %s from tag %s", a.Name, tag)}, nil
}

type setEnvInput struct {
	Name string     `json:"name" jsonschema:"app name"`
	Vars envvar.Map `json:"vars" jsonschema:"the full set of app-level environment variables — this REPLACES whatever was set before"`
}

func (t *Tools) setEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	a, err := t.svc.SetEnv(ctx, t.caller, in.Name, in.Vars)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for app %s", a.Name)}, nil
}

type logsInput struct {
	Name string `json:"name" jsonschema:"app name"`
	Tail string `json:"tail,omitempty" jsonschema:"number of trailing lines to return, e.g. \"500\", or \"all\" for the full log (default \"200\")"`
}

func (t *Tools) logs(ctx context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, any, error) {
	tail := in.Tail
	if tail == "" {
		tail = mcpDefaultLogTail
	}
	rc, err := t.svc.Logs(ctx, t.caller, in.Name, tail)
	if err != nil {
		return nil, nil, err
	}
	defer rc.Close()

	// Containers run without a TTY, so the Engine multiplexes
	// stdout/stderr behind an 8-byte binary frame header per chunk —
	// demultiplex it before this is readable at all.
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, rc); err != nil {
		return nil, nil, fmt.Errorf("read logs: %w", err)
	}

	text := buf.String()
	if len(text) > maxMCPLogBytes {
		text = "...[truncated; showing the most recent output]...\n" + text[len(text)-maxMCPLogBytes:]
	}
	if text == "" {
		text = "(no log output)"
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}
