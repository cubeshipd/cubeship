package org

import (
	"context"
	"fmt"

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
		Name:        "create_org",
		Description: "Create a new organization. Super-admin only.",
	}, t.create)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_orgs",
		Description: "List organizations you belong to (or every organization, if you're a super-admin).",
	}, t.list)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "delete_org",
		Description: "Delete an organization and everything inside it: every app is stopped and removed, then its projects, environments and memberships. Super-admin only, and cannot be undone.",
	}, t.delete)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_org_user",
		Description: "Add a user to an organization, creating them if they're new. Requires admin role in the organization.",
	}, t.createUser)
}

type createInput struct {
	Slug string `json:"slug" jsonschema:"short identifier used in URLs and registry paths: lowercase letters, digits and dashes. Permanent - it cannot be changed later"`
}

func (t *Tools) create(ctx context.Context, _ *mcp.CallToolRequest, in createInput) (*mcp.CallToolResult, Response, error) {
	created, err := t.svc.Create(ctx, t.caller, in.Slug)
	if err != nil {
		return nil, Response{}, err
	}
	return nil, Response{Slug: created.Slug}, nil
}

func (t *Tools) list(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []Response, error) {
	orgs, err := t.svc.List(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	return nil, toResponses(orgs), nil
}

type orgScopedInput struct {
	Org string `json:"org" jsonschema:"organization slug"`
}

func (t *Tools) delete(ctx context.Context, _ *mcp.CallToolRequest, in orgScopedInput) (*mcp.CallToolResult, user.ActionResult, error) {
	o, err := t.svc.Delete(ctx, t.caller, in.Org)
	if err != nil {
		return nil, user.ActionResult{}, err
	}
	return nil, user.ActionResult{Message: fmt.Sprintf("deleted organization %s", o.Slug)}, nil
}

type createUserInput struct {
	Org      string `json:"org" jsonschema:"organization slug"`
	Username string `json:"username"`
	Role     string `json:"role,omitempty" jsonschema:"\"admin\" or \"member\" (default \"member\")"`
}

func (t *Tools) createUser(ctx context.Context, _ *mcp.CallToolRequest, in createUserInput) (*mcp.CallToolResult, CreateUserResponse, error) {
	if in.Role == "" {
		in.Role = string(RoleMember)
	}
	o, err := t.svc.Resolve(ctx, t.caller, in.Org, RoleAdmin)
	if err != nil {
		return nil, CreateUserResponse{}, err
	}
	apiKey, err := t.svc.AddUser(ctx, t.caller, o, in.Username, Role(in.Role))
	if err != nil {
		return nil, CreateUserResponse{}, err
	}
	return nil, CreateUserResponse{Username: in.Username, Org: o.Slug, Role: in.Role, APIKey: apiKey}, nil
}
