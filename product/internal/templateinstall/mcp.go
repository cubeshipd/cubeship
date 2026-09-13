package templateinstall

import (
	"context"
	"encoding/json"
	"net/url"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// **The install tool returns no generated secret.** The API hands them
// back once, to the person who asked; through MCP they would pass into a
// model's context for no purpose, since each one is already in the
// variables of the app that uses it.
type Tools struct {
	svc    *Service
	caller *user.User
}

func NewTools(svc *Service, caller *user.User) *Tools {
	return &Tools{svc: svc, caller: caller}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_templates",
		Description: "Search the template catalog at cubeship.dev: ready-made apps with the databases and stores they need, like Umami, n8n or Grafana. q matches a template's name, description or tags.",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "install_template",
		Description: "Install a template from the catalog: create its project and environment when they do not exist, its databases, stores and apps, and deploy the apps. It runs in the background — follow it with get_template_install. Anything that fails is undone. A secret input with generate is generated when left out, and is not returned: it is in the variables of the app that uses it. Requires the admin role.",
	}, t.install)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_template_install",
		Description: "How an install started by install_template is going: its status (running, succeeded, failed — a failed install has already been undone), the step it is on, and what it created.",
	}, t.get)
}

type listInput struct {
	Q      string `json:"q,omitempty" jsonschema:"search the name, description and tags"`
	Tag    string `json:"tag,omitempty" jsonschema:"only templates with this tag"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

type listOutput struct {
	Templates  []any   `json:"templates"`
	NextCursor *string `json:"next_cursor"`
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, in listInput) (*mcp.CallToolResult, listOutput, error) {
	query := url.Values{}
	query.Set("q", in.Q)
	query.Set("tag", in.Tag)
	query.Set("cursor", in.Cursor)
	page, err := t.svc.Templates(ctx, t.caller, query)
	if err != nil {
		return nil, listOutput{}, err
	}
	var out listOutput
	if err := json.Unmarshal(page, &out); err != nil {
		return nil, listOutput{}, err
	}
	if out.Templates == nil {
		out.Templates = []any{}
	}
	return nil, out, nil
}

type installInput struct {
	Owner         string            `json:"owner" jsonschema:"the template repository's owner on GitHub, from list_templates"`
	Repo          string            `json:"repo" jsonschema:"the template repository's name"`
	Release       string            `json:"release,omitempty" jsonschema:"a release tag. Defaults to the newest the catalog accepted"`
	Project       string            `json:"project,omitempty" jsonschema:"the project to install into, created when it does not exist. Defaults to the template's suggestion"`
	Environment   string            `json:"environment,omitempty" jsonschema:"the environment inside it, created when it does not exist. Defaults to the template's, usually production"`
	Inputs        map[string]string `json:"inputs,omitempty" jsonschema:"answers to the template's inputs, by key: a domain, a choice, a store's name"`
	DatabaseNames map[string]string `json:"database_names,omitempty" jsonschema:"rename a database the template declares, by its key. Database names are unique on the instance"`
	StoreNames    map[string]string `json:"store_names,omitempty" jsonschema:"rename an object store the template declares, by its key"`
	AppNames      map[string]string `json:"app_names,omitempty" jsonschema:"rename an app the template declares, by its key"`
}

func (t *Tools) install(ctx context.Context, _ *mcp.CallToolRequest, in installInput) (*mcp.CallToolResult, Response, error) {
	started, _, err := t.svc.Install(ctx, t.caller, Request{
		Owner: in.Owner, Repo: in.Repo, Release: in.Release,
		Project: in.Project, Environment: in.Environment,
		Databases: in.DatabaseNames, Stores: in.StoreNames, Apps: in.AppNames,
		Inputs: in.Inputs,
	})
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(started), nil
}

type getInput struct {
	ID int64 `json:"id" jsonschema:"the install's id, from install_template"`
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, Response, error) {
	found, err := t.svc.Get(ctx, t.caller, in.ID)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(found), nil
}
