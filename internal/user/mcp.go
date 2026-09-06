package user

import (
	"context"
	"errors"
	"fmt"

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
		Description: "Report the identity (username, super-admin status) of the API key this MCP session is using.",
	}, t.whoAmI)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_api_key",
		Description: `Issue an additional, independent API key for yourself under a given name (e.g. "mcp", "laptop") — it coexists with every key you already hold.`,
	}, t.createAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_api_keys",
		Description: "List metadata for every API key you hold (id, name, timestamps). Key values are never shown again after creation.",
	}, t.listAPIKeys)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "revoke_api_key",
		Description: "Revoke one of your own API keys by id. Refused if it's your only remaining key.",
	}, t.revokeAPIKey)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rotate_my_api_key",
		Description: "Replace the API key this MCP session is currently using with a freshly generated one. WARNING: the key authenticating this very session stops working immediately — this session's next call will fail. Every other key you hold is unaffected.",
	}, t.rotateMyAPIKey)
}

func (t *Tools) whoAmI(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, WhoAmIResponse, error) {
	// No password flag here. It exists for the account screen, which
	// has a revoke button beside it; an agent has nothing to do with
	// how its caller signs in.
	return nil, WhoAmIResponse{Username: t.caller.Username, Role: t.caller.Role}, nil
}

type createAPIKeyInput struct {
	Name string `json:"name" jsonschema:"a label to recognize this key by later, e.g. \"mcp\" or \"laptop\""`
}

type createAPIKeyOutput struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	APIKey string `json:"api_key"`
}

func (t *Tools) createAPIKey(ctx context.Context, _ *mcp.CallToolRequest, in createAPIKeyInput) (*mcp.CallToolResult, createAPIKeyOutput, error) {
	created, generated, err := t.svc.CreateAPIKey(ctx, t.caller, in.Name)
	if err != nil {
		return nil, createAPIKeyOutput{}, err
	}
	return nil, createAPIKeyOutput{ID: created.ID, Name: created.Name, APIKey: generated}, nil
}

func (t *Tools) listAPIKeys(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, []APIKeyResponse, error) {
	keys, err := t.svc.ListAPIKeys(ctx, t.caller)
	if err != nil {
		return nil, nil, err
	}
	return nil, toAPIKeyResponses(keys, t.keyHash), nil
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
