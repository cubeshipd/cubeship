package app

import (
	"bytes"
	"context"
	"fmt"
	"time"

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
		Description: "Get one app by reference: its domain, registry push path, status, and which organization/project/environment it lives in.",
	}, t.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "deploy_app",
		Description: `Manually redeploy an app from an image tag already pushed to its registry path (tag defaults to "latest"). Waits for the deploy to finish and reports the outcome; if this call times out first, the deploy carries on regardless — check it with get_app_deployments. Requires member role in the organization.`,
	}, t.deploy)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_env",
		Description: "Read an app's environment variables: the ones set on the app itself, and the effective set its container runs with (project, then environment, then app — each overriding the last), with the source of every value. Read this before changing anything.",
	}, t.getEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_app_env",
		Description: "Add, change or remove an app's own environment variables. Only the keys you name are touched — variables you don't mention are left alone. These are layered on top of (and override) the app's environment's and project's variables. Requires member role in the organization.",
	}, t.setEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_deployments",
		Description: "List an app's recent deploys, newest first: whether each succeeded, and why it failed if it did. A deploy runs detached from the call that started it, so this is how you find out how one ended.",
	}, t.deployments)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_app",
		Description: "Delete an app and stop the container serving it. Images already pushed stay in the registry. This cannot be undone. Requires member role in the organization.",
	}, t.delete)
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
	App string `json:"app" jsonschema:"app reference: org/project/environment/app, or org/project/app for production"`
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, Response, error) {
	a, err := t.svc.ResolveString(ctx, t.caller, in.App, orgRoleMember)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(a), nil
}

type deployInput struct {
	App string `json:"app" jsonschema:"app reference: org/project/environment/app, or org/project/app for production"`
	Tag string `json:"tag,omitempty" jsonschema:"image tag already pushed to the app's registry path (default \"latest\")"`
}

func (t *Tools) deploy(ctx context.Context, _ *mcp.CallToolRequest, in deployInput) (*mcp.CallToolResult, user.ActionResult, error) {
	tag := in.Tag
	if tag == "" {
		tag = "latest"
	}
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	a, deployment, err := t.svc.Deploy(ctx, t.caller, ref, tag)
	if err != nil {
		return nil, user.ActionResult{}, err
	}

	// The deploy runs detached, so waiting here is a convenience, not a
	// dependency: if this call times out the deploy carries on, and
	// get_app_deployments reports how it ended.
	finished, waitErr := t.svc.WaitForDeployment(ctx, t.caller, ref, deployment.ID)
	if waitErr != nil {
		return nil, user.ActionResult{Message: fmt.Sprintf(
			"deploy %d of %s from tag %s is still running; check it with get_app_deployments",
			deployment.ID, a.Name, tag)}, nil
	}
	if finished.Status == DeploymentFailed {
		return nil, user.ActionResult{}, fmt.Errorf("deploy of %s from tag %s failed: %s", a.Name, tag, finished.Error)
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deployed %s from tag %s", a.Name, tag)}, nil
}

type deploymentOutput struct {
	ID        int64  `json:"id"`
	Status    string `json:"status" jsonschema:"pending, succeeded or failed"`
	Image     string `json:"image"`
	Error     string `json:"error,omitempty" jsonschema:"why it failed, when it did"`
	CreatedAt string `json:"created_at"`
}

func (t *Tools) deployments(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, []deploymentOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, nil, err
	}
	history, err := t.svc.Deployments(ctx, t.caller, ref)
	if err != nil {
		return nil, nil, err
	}
	out := make([]deploymentOutput, 0, len(history))
	for _, d := range history {
		out = append(out, deploymentOutput{
			ID: d.ID, Status: d.Status, Image: d.ImageRef, Error: d.Error,
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
		})
	}
	return nil, out, nil
}

func (t *Tools) delete(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, user.ActionResult, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	if _, err := t.svc.Delete(ctx, t.caller, ref); err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted app %s", ref)}, nil
}

type envOutput struct {
	Vars      envvar.Map        `json:"vars" jsonschema:"the variables set on the app itself"`
	Effective []envvar.Resolved `json:"effective" jsonschema:"every variable the container actually runs with, and the level that set it"`
}

func (t *Tools) getEnv(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, envOutput, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, envOutput{}, err
	}
	own, effective, err := t.svc.Env(ctx, t.caller, ref)
	if err != nil {
		return nil, envOutput{}, err
	}
	return nil, envOutput{Vars: own, Effective: effective}, nil
}

type setEnvInput struct {
	App   string     `json:"app" jsonschema:"app reference: org/project/environment/app, or org/project/app for production"`
	Set   envvar.Map `json:"set,omitempty" jsonschema:"variables to add or overwrite"`
	Unset []string   `json:"unset,omitempty" jsonschema:"names of variables to remove"`
}

// setEnv merges rather than replaces. An agent that means to change one
// variable must not be able to erase the rest by omitting them, which is
// exactly what the old replace-everything tool made easy.
func (t *Tools) setEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	if len(in.Set) == 0 && len(in.Unset) == 0 {
		return nil, user.ActionResult{}, fmt.Errorf("give set, unset, or both")
	}
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	a, err := t.svc.MergeEnv(ctx, t.caller, ref, in.Set, in.Unset)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for app %s", a.Name)}, nil
}

type logsInput struct {
	App  string `json:"app" jsonschema:"app reference: org/project/environment/app, or org/project/app for production"`
	Tail string `json:"tail,omitempty" jsonschema:"number of trailing lines to return, e.g. \"500\", or \"all\" for the full log (default \"200\")"`
}

func (t *Tools) logs(ctx context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, any, error) {
	tail := in.Tail
	if tail == "" {
		tail = mcpDefaultLogTail
	}
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, nil, err
	}
	rc, err := t.svc.Logs(ctx, t.caller, ref, tail)
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
