package templateinstall

import (
	"context"
	"encoding/json"
	"net/url"

	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// **No tool returns a generated secret.** The API hands them back once,
// to the person who asked; through MCP they would pass into a model's
// context for no purpose, since each one is already in the variables of
// the app that uses it.
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
		Name:        "list_template_releases",
		Description: "List the releases of a template the catalog accepted, newest first: the versions install_template can be given as release.",
	}, t.releases)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "install_template",
		Description: "Install a template from the catalog: create its project and environment when they do not exist, its databases, stores and apps, and deploy the apps. It runs in the background — follow it with get_template_install. Anything that fails is undone. A secret input with generate is generated when left out, and is not returned: it is in the variables of the app that uses it. Requires the admin role.",
	}, t.install)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_template_installs",
		Description: "List the templates installed on this instance: the release each is on, whether a newer one is available, and whether a run is changing it now.",
	}, t.installs)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_template_install",
		Description: "One installation: what it created, its recent runs — install, update, uninstall — with the step a running one is on and why a failed one failed. A failed run has already been undone.",
	}, t.get)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "preview_template_update",
		Description: "What updating an installation to a release would do, without doing it: each app, database, store, variable, domain and attachment it would create or change, what it leaves in place, and any question it would need answered. Call this before update_template_install. Requires the admin role.",
	}, t.preview)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_template_install",
		Description: "Update an installation to a release — the newest by default — in the background. It applies what changed and deletes nothing; if any step fails, the installation is put back as it was. Answer the questions preview_template_update listed in inputs. Requires the admin role.",
	}, t.update)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "uninstall_template_install",
		Description: "Uninstall an installation in the background: its apps are deleted, and the project and environment it created once they are empty. Its databases and object stores are kept unless delete_data is true, which deletes them and their data permanently. Requires the admin role.",
	}, t.uninstall)
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
	return nil, toResponse(*started), nil
}

type installsOutput struct {
	Installs []Response `json:"installs"`
}

func (t *Tools) installs(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, installsOutput, error) {
	found, err := t.svc.Installs(ctx, t.caller)
	if err != nil {
		return nil, installsOutput{}, err
	}
	out := installsOutput{Installs: []Response{}}
	for _, i := range found {
		out.Installs = append(out.Installs, toResponse(i))
	}
	return nil, out, nil
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"the installation's id, from list_template_installs or install_template"`
}

func (t *Tools) get(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, Response, error) {
	found, err := t.svc.Get(ctx, t.caller, in.ID)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(*found), nil
}

type previewInput struct {
	ID      int64  `json:"id" jsonschema:"the installation's id"`
	Release string `json:"release,omitempty" jsonschema:"a release tag. Defaults to the newest the catalog accepted"`
}

func (t *Tools) preview(ctx context.Context, _ *mcp.CallToolRequest, in previewInput) (*mcp.CallToolResult, Preview, error) {
	preview, err := t.svc.PreviewUpdate(ctx, t.caller, in.ID, in.Release)
	if err != nil {
		return nil, Preview{}, err
	}
	return nil, *preview, nil
}

type updateInput struct {
	ID      int64             `json:"id" jsonschema:"the installation's id"`
	Release string            `json:"release,omitempty" jsonschema:"a release tag. Defaults to the newest the catalog accepted"`
	Inputs  map[string]string `json:"inputs,omitempty" jsonschema:"answers to the questions preview_template_update listed, by key"`
}

func (t *Tools) update(ctx context.Context, _ *mcp.CallToolRequest, in updateInput) (*mcp.CallToolResult, RunResponse, error) {
	run, _, err := t.svc.Update(ctx, t.caller, in.ID, UpdateRequest{Release: in.Release, Inputs: in.Inputs})
	if err != nil {
		return nil, RunResponse{}, err
	}
	return nil, toRunResponse(run), nil
}

type uninstallInput struct {
	ID         int64 `json:"id" jsonschema:"the installation's id"`
	DeleteData bool  `json:"delete_data,omitempty" jsonschema:"also delete the databases and object stores it created, and everything in them, permanently. Defaults to false"`
}

func (t *Tools) uninstall(ctx context.Context, _ *mcp.CallToolRequest, in uninstallInput) (*mcp.CallToolResult, RunResponse, error) {
	run, err := t.svc.Uninstall(ctx, t.caller, in.ID, !in.DeleteData)
	if err != nil {
		return nil, RunResponse{}, err
	}
	return nil, toRunResponse(run), nil
}

type releasesInput struct {
	Owner string `json:"owner" jsonschema:"the template repository's owner on GitHub, from list_templates"`
	Repo  string `json:"repo" jsonschema:"the template repository's name"`
}

type releasesOutput struct {
	Releases []ReleaseOption `json:"releases"`
}

func (t *Tools) releases(ctx context.Context, _ *mcp.CallToolRequest, in releasesInput) (*mcp.CallToolResult, releasesOutput, error) {
	out, err := t.svc.Releases(ctx, t.caller, in.Owner, in.Repo)
	if err != nil {
		return nil, releasesOutput{}, err
	}
	return nil, releasesOutput{Releases: out}, nil
}
