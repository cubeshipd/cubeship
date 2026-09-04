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
		Name:        "update_project",
		Description: "Rename a project or change its description. A field you leave out is left as it was. The slug cannot be changed — no slug in Cubeship can, once the resource exists, because it is a path component of every app's registry reference. Requires admin role in the organization.",
	}, t.update)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_project",
		Description: "Delete a project and the environments inside it. Refused while any app still lives in the project — delete those first. Requires admin role in the organization.",
	}, t.delete)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_project_env",
		Description: "Read the environment variables set on a project. Every environment and every app below inherits them.",
	}, t.getEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_project_env",
		Description: "Add, change or remove environment variables shared by every environment (and every app) in a project. Only the keys you name are touched. Requires admin role in the organization.",
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
		Name:        "get_environment_env",
		Description: "Read the environment variables set on one environment, plus the effective set an app there inherits (the project's, overridden by this environment's) with the source of every value.",
	}, t.getEnvironmentEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "set_environment_env",
		Description: "Add, change or remove environment variables shared by every app in one environment. Only the keys you name are touched. Requires admin role in the organization.",
	}, t.setEnvironmentEnv)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "update_environment",
		Description: "Rename an environment or change its description. A field you leave out is left as it was. The slug cannot be changed — it is part of every app reference in the environment. Requires admin role in the organization.",
	}, t.updateEnvironment)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_environment",
		Description: `Delete an environment. Refused for the "production" environment, and refused if the environment still has apps in it. Requires admin role in the organization.`,
	}, t.deleteEnvironment)
}

type createInput struct {
	Org  string `json:"org" jsonschema:"organization slug"`
	Slug string `json:"slug" jsonschema:"short identifier used in URLs: lowercase letters, digits and dashes. Permanent - it cannot be changed later"`
	Name string `json:"name,omitempty" jsonschema:"optional display name; leave out and it is derived from the slug, and can be edited afterwards"`
}

func (t *Tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, Response, error) {
	p, env, err := t.svc.Create(ctx, t.caller, in.Org, in.Slug, in.Name)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, Response{Slug: p.Slug, Name: p.Name, Description: p.Description, Environments: []string{env.Slug}}, nil
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

type envOutput struct {
	Vars      envvar.Map        `json:"vars" jsonschema:"the variables set at this level"`
	Effective []envvar.Resolved `json:"effective,omitempty" jsonschema:"the inherited result, and the level that set each value"`
}

func (t *Tools) getEnv(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, envOutput, error) {
	vars, err := t.svc.Env(ctx, t.caller, in.Org, in.Project)
	if err != nil {
		return nil, envOutput{}, err
	}
	return nil, envOutput{Vars: vars}, nil
}

type updateInput struct {
	Org         string  `json:"org" jsonschema:"organization slug"`
	Project     string  `json:"project" jsonschema:"project slug"`
	Name        *string `json:"name,omitempty" jsonschema:"the new name; leave out to keep the current one"`
	Description *string `json:"description,omitempty" jsonschema:"what the project is for; leave out to keep it, send empty to clear it"`
}

func (t *Tools) update(ctx context.Context, _ *mcp.CallToolRequest, in updateInput) (*mcp.CallToolResult, Response, error) {
	if in.Name == nil && in.Description == nil {
		return nil, Response{}, fmt.Errorf("give name, description, or both")
	}
	p, err := t.svc.Update(ctx, t.caller, in.Org, in.Project, in.Name, in.Description)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, toResponse(p), nil
}

func (t *Tools) delete(ctx context.Context, _ *mcp.CallToolRequest, in projectScopedInput) (*mcp.CallToolResult, user.ActionResult, error) {
	p, err := t.svc.Delete(ctx, t.caller, in.Org, in.Project)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted project %s", p.Slug)}, nil
}

type setEnvInput struct {
	Org     string     `json:"org" jsonschema:"organization slug"`
	Project string     `json:"project" jsonschema:"project slug"`
	Set     envvar.Map `json:"set,omitempty" jsonschema:"variables to add or overwrite"`
	Unset   []string   `json:"unset,omitempty" jsonschema:"names of variables to remove"`
}

func (t *Tools) setEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	if len(in.Set) == 0 && len(in.Unset) == 0 {
		return nil, user.ActionResult{}, fmt.Errorf("give set, unset, or both")
	}
	p, err := t.svc.MergeEnv(ctx, t.caller, in.Org, in.Project, in.Set, in.Unset)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("updated env for project %s", p.Slug)}, nil
}

type createEnvironmentInput struct {
	Org     string `json:"org" jsonschema:"organization slug"`
	Project string `json:"project" jsonschema:"project slug"`
	Slug    string `json:"slug" jsonschema:"short identifier used in URLs and as the environment name apps request. Permanent - it cannot be changed later"`
	Name    string `json:"name,omitempty" jsonschema:"optional display name; leave out and it is derived from the slug, and can be edited afterwards"`
}

func (t *Tools) createEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in createEnvironmentInput) (*mcp.CallToolResult, EnvironmentResponse, error) {
	env, err := t.svc.CreateEnvironment(ctx, t.caller, in.Org, in.Project, in.Slug, in.Name)
	if err != nil {
		return nil, EnvironmentResponse{}, err
	}
	return nil, toEnvironmentResponse(env), nil
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
	Set         envvar.Map `json:"set,omitempty" jsonschema:"variables to add or overwrite"`
	Unset       []string   `json:"unset,omitempty" jsonschema:"names of variables to remove"`
}

func (t *Tools) getEnvironmentEnv(ctx context.Context, _ *mcp.CallToolRequest, in environmentScopedInput) (*mcp.CallToolResult, envOutput, error) {
	vars, effective, err := t.svc.EnvironmentEnv(ctx, t.caller, in.Org, in.Project, in.Environment)
	if err != nil {
		return nil, envOutput{}, err
	}
	return nil, envOutput{Vars: vars, Effective: effective}, nil
}

func (t *Tools) setEnvironmentEnv(ctx context.Context, _ *mcp.CallToolRequest, in setEnvironmentEnvInput) (*mcp.CallToolResult, user.ActionResult, error) {
	if len(in.Set) == 0 && len(in.Unset) == 0 {
		return nil, user.ActionResult{}, fmt.Errorf("give set, unset, or both")
	}
	env, err := t.svc.MergeEnvironmentEnv(ctx, t.caller, in.Org, in.Project, in.Environment, in.Set, in.Unset)
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

type updateEnvironmentInput struct {
	Org         string  `json:"org" jsonschema:"organization slug"`
	Project     string  `json:"project" jsonschema:"project slug"`
	Environment string  `json:"environment" jsonschema:"environment slug"`
	Name        *string `json:"name,omitempty" jsonschema:"the new name; leave out to keep the current one"`
	Description *string `json:"description,omitempty" jsonschema:"what this stage is for; leave out to keep it, send empty to clear it"`
}

func (t *Tools) updateEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in updateEnvironmentInput) (*mcp.CallToolResult, EnvironmentResponse, error) {
	if in.Name == nil && in.Description == nil {
		return nil, EnvironmentResponse{}, fmt.Errorf("give name, description, or both")
	}
	e, err := t.svc.UpdateEnvironment(ctx, t.caller, in.Org, in.Project, in.Environment, in.Name, in.Description)
	if err != nil {
		return nil, EnvironmentResponse{}, err
	}
	return nil, toEnvironmentResponse(e), nil
}

func (t *Tools) deleteEnvironment(ctx context.Context, _ *mcp.CallToolRequest, in environmentScopedInput) (*mcp.CallToolResult, user.ActionResult, error) {
	env, err := t.svc.DeleteEnvironment(ctx, t.caller, in.Org, in.Project, in.Environment)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted environment %s", env.Slug)}, nil
}
