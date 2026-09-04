package project

import (
	"context"
	"fmt"

	"cubeship/internal/envvar"
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
		Name:        "create_project",
		Description: `Create a project within an organization. Comes with a "production" environment, which can never be deleted. Requires admin role in the organization.`,
	}, t.create)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_projects",
		Description: "List the projects in an organization.",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_project_env",
		Description: "Set environment variables shared by every environment (and every app) in a project. Replaces the full set of project-level variables. Requires admin role in the organization.",
	}, t.setEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_environment",
		Description: "Create an additional environment within a project. Requires admin role in the organization.",
	}, t.createEnvironment)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_environments",
		Description: "List the environments in a project.",
	}, t.listEnvironments)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_environment_env",
		Description: "Set environment variables shared by every app in one environment. Replaces the full set of environment-level variables. Requires admin role in the organization.",
	}, t.setEnvironmentEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_environment",
		Description: `Delete an environment. Refused for the "production" environment, and refused if the environment still has apps in it. Requires admin role in the organization.`,
	}, t.deleteEnvironment)
}

type createInput struct {
	Org  string `json:"org" jsonschema:"organization slug"`
	Slug string `json:"slug" jsonschema:"short identifier used in URLs: lowercase letters, digits and dashes"`
	Name string `json:"name"`
}

func (t *Tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, Response, error) {
	p, env, err := t.svc.Create(ctx, t.caller, in.Org, in.Slug, in.Name)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, Response{Slug: p.Slug, Name: p.Name, Environments: []string{env.Slug}}, nil
}

type orgScopedInput struct {
	Org string `json:"org" jsonschema:"organization slug"`
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, in orgScopedInput) (*mcp.CallToolResult, []Response, error) {
	projects, err := t.svc.List(ctx, t.caller, in.Org)
	if err != nil {
		return nil, nil, err
	}
	return nil, toResponses(projects), nil
}

type setEnvInput struct {
	Org     string     `json:"org" jsonschema:"organization slug"`
	Project string     `json:"project" jsonschema:"project slug"`
	Vars    envvar.Map `json:"vars" jsonschema:"the full set of project-level environment variables — this REPLACES whatever was set before"`
}

func (t *Tools) setEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	p, err := t.svc.SetEnv(ctx, t.caller, in.Org, in.Project, in.Vars)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for project %s", p.Slug)}, nil
}

type createEnvironmentInput struct {
	Org     string `json:"org" jsonschema:"organization slug"`
	Project string `json:"project" jsonschema:"project slug"`
	Slug    string `json:"slug" jsonschema:"short identifier used in URLs and as the environment name apps request"`
	Name    string `json:"name"`
}

func (t *Tools) createEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in createEnvironmentInput) (*mcp.CallToolResult, EnvironmentResponse, error) {
	env, err := t.svc.CreateEnvironment(ctx, t.caller, in.Org, in.Project, in.Slug, in.Name)
	if err != nil {
		return nil, EnvironmentResponse{}, err
	}
	return nil, EnvironmentResponse{Slug: env.Slug, Name: env.Name}, nil
}

type projectScopedInput struct {
	Org     string `json:"org" jsonschema:"organization slug"`
	Project string `json:"project" jsonschema:"project slug"`
}

func (t *Tools) listEnvironments(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, []EnvironmentResponse, error) {
	envs, err := t.svc.ListEnvironments(ctx, t.caller, in.Org, in.Project)
	if err != nil {
		return nil, nil, err
	}
	return nil, toEnvironmentResponses(envs), nil
}

type setEnvironmentEnvInput struct {
	Org         string     `json:"org" jsonschema:"organization slug"`
	Project     string     `json:"project" jsonschema:"project slug"`
	Environment string     `json:"environment" jsonschema:"environment slug"`
	Vars        envvar.Map `json:"vars" jsonschema:"the full set of environment-level variables — this REPLACES whatever was set before"`
}

func (t *Tools) setEnvironmentEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvironmentEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	env, err := t.svc.SetEnvironmentEnv(ctx, t.caller, in.Org, in.Project, in.Environment, in.Vars)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for environment %s", env.Slug)}, nil
}

type environmentScopedInput struct {
	Org         string `json:"org" jsonschema:"organization slug"`
	Project     string `json:"project" jsonschema:"project slug"`
	Environment string `json:"environment" jsonschema:"environment slug"`
}

func (t *Tools) deleteEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in environmentScopedInput) (*mcp.CallToolResult, user.ActionResult, error) {
	env, err := t.svc.DeleteEnvironment(ctx, t.caller, in.Org, in.Project, in.Environment)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted environment %s", env.Slug)}, nil
}
