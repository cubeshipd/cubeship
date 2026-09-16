package user

import (
	"context"
	"errors"
	"fmt"

	"cubeship/internal/mcpx"
	"cubeship/internal/platform/database"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tools is this module's MCP surface: the same use cases the HTTP
// handlers expose, reached by an agent instead of a client.
//
// It closes over the caller — the MCP server is built per request, so a
// tool never has to re-derive who is calling and no session can be reused
// across users.
type Tools struct {
	svc     *Service
	caller  *User
	keyHash string
}

func NewTools(svc *Service, caller *User, keyHash string) *Tools {
	return &Tools{svc: svc, caller: caller, keyHash: keyHash}
}

func (t *Tools) Register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "whoami",
		Description: "Report who the API key this MCP session is using belongs to: the username, and the role it holds on this instance — `admin` or `member`.",
	}, t.whoAmI)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_api_key",
		Description: `Issue an additional, independent API key for yourself under a given name (e.g. "mcp", "laptop") — it coexists with every key you already hold. role narrows the key to an access role from list_roles; without one it carries everything you may. A key can never create one that reaches more than it does.`,
	}, t.createAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_roles",
		Description: "List the access roles on this instance: each one's name and grants — for every kind of resource, a level (none, view or manage), whether it reads secrets, and which items (null for every one). A role narrows what a member or an API key reaches.",
	}, t.listRoles)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "List metadata for every API key you hold (id, name, timestamps). Key values are never shown again after creation.",
	}, t.listAPIKeys)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "Revoke one of your own API keys by id. **Never refused, including your last one**: a key that has leaked has to be able to go now, and being made to mint a replacement first would keep the leaked one live for as long as that took.\n\nSo what it costs is yours to weigh before calling it. Revoking the key this session is authenticating with stops this session at once — the next call fails. Revoking the last one leaves the account with no key at all, and whether that leaves a way in depends on whether it has a password, which this surface does not tell you.",
	}, t.revokeAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rotate_my_api_key",
		Description: "Replace the API key this MCP session is currently using with a freshly generated one. WARNING: the key authenticating this very session stops working immediately — this session's next call will fail. Every other key you hold is unaffected.",
	}, t.rotateMyAPIKey)
}

func (t *Tools) whoAmI(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, WhoAmIResponse, error) {
	// No password flag here. It exists for the account screen, which
	// has a revoke button beside it; an agent has nothing to do with
	// how its caller signs in.
	return nil, WhoAmIResponse{Username: t.caller.Username, Role: t.caller.Role, Key: t.svc.KeyResponse(ctx, t.caller), AccessRole: t.svc.RoleName(ctx, t.caller.AccessRoleID), Grants: grantsOf(t.caller)}, nil
}

type createAPIKeyInput struct {
	Name string `json:"name" jsonschema:"a label to recognize this key by later, e.g. \"mcp\" or \"laptop\""`
	Role string `json:"role,omitempty" jsonschema:"the name of an access role narrowing the key, from list_roles; omitted is everything the key calling this reaches"`
}

type createAPIKeyOutput struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	APIKey     string `json:"api_key"`
	AccessRole string `json:"access_role,omitempty"`
}

func (t *Tools) createAPIKey(ctx context.Context, _ *mcp.CallToolRequest, in createAPIKeyInput) (*mcp.CallToolResult, createAPIKeyOutput, error) {
	req := KeyRequest{Name: in.Name}
	if in.Role != "" {
		role, err := t.svc.RoleByName(ctx, t.caller, in.Role)
		if err != nil {
			return nil, createAPIKeyOutput{}, fmt.Errorf("role %q: %w", in.Role, err)
		}
		req.AccessRoleID = role.ID
	}
	created, generated, err := t.svc.CreateAPIKey(ctx, t.caller, req)
	if err != nil {
		return nil, createAPIKeyOutput{}, err
	}
	return nil, createAPIKeyOutput{
		ID: created.ID, Name: created.Name, APIKey: generated,
		AccessRole: t.svc.RoleName(ctx, created.AccessRoleID),
	}, nil
}

func (t *Tools) listRoles(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpx.List[RoleResponse], error) {
	roles, err := t.svc.ListRoles(ctx, t.caller)
	if err != nil {
		return nil, mcpx.List[RoleResponse]{}, err
	}
	out := make([]RoleResponse, 0, len(roles))
	for _, r := range roles {
		out = append(out, toRoleResponse(r))
	}
	return nil, mcpx.Of(out), nil
}

func (t *Tools) listAPIKeys(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, mcpx.List[APIKeyResponse], error) {
	keys, err := t.svc.ListAPIKeys(ctx, t.caller)
	if err != nil {
		return nil, mcpx.List[APIKeyResponse]{}, err
	}
	return nil, mcpx.Of(toAPIKeyResponses(keys, t.keyHash, t.svc.RoleNames(ctx))), nil
}

type revokeAPIKeyInput struct {
	ID int64 `json:"id" jsonschema:"the key's id, from list_api_keys"`
}

// ActionResult is the output shape for a tool that performs a write with
// no natural resource to hand back — a one-line confirmation of what
// happened. Every module's tools share it.
type ActionResult struct {
	Message string `json:"message"`
}

func (t *Tools) revokeAPIKey(ctx context.Context, _ *mcp.CallToolRequest, in revokeAPIKeyInput) (*mcp.CallToolResult, ActionResult, error) {
	if err := t.svc.RevokeAPIKey(ctx, t.caller, in.ID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ActionResult{}, fmt.Errorf("api key %d not found", in.ID)
		}
		return nil, ActionResult{}, err
	}
	return nil, ActionResult{Message: fmt.Sprintf("revoked api key %d", in.ID)}, nil
}

type rotateMyAPIKeyOutput struct {
	APIKey string `json:"api_key"`
}

func (t *Tools) rotateMyAPIKey(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, rotateMyAPIKeyOutput, error) {
	key, err := t.svc.RotateAPIKey(ctx, t.caller, t.keyHash)
	if err != nil {
		return nil, rotateMyAPIKeyOutput{}, err
	}
	return nil, rotateMyAPIKeyOutput{APIKey: key}, nil
}
