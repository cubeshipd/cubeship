package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const deployHookUsage = " POST to the url, with no credentials, to deploy the app: ?tag=x.y.z for an app that runs a published image (saved on an external or pinned app, so later deploys stay on it), ?ref=<branch, tag or commit> for one that builds, and ?wait=true to answer once the deploy has finished. Requires the member role, and the admin role for an app that builds."

func (t *Tools) registerDeployHook(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_app_deploy_hook",
		Description: "Read an app's deploy webhook: its url (null while the instance has no domain) and the token in it. Anyone holding the url can deploy the app, so treat it as a secret." + deployHookUsage,
	}, t.getDeployHook)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "rotate_app_deploy_hook",
		Description: "Give an app's deploy webhook a new token and return the new url. The old url stops working at once, so every CI that used it has to be given the new one." + deployHookUsage,
	}, t.rotateDeployHook)
}

func (t *Tools) getDeployHook(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, DeployHookResponse, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, DeployHookResponse{}, err
	}
	hook, err := t.svc.DeployHook(ctx, t.caller, ref)
	if err != nil {
		return nil, DeployHookResponse{}, err
	}
	return nil, toDeployHookResponse(hook), nil
}

func (t *Tools) rotateDeployHook(ctx context.Context, _ *mcp.CallToolRequest, in nameInput) (*mcp.CallToolResult, DeployHookResponse, error) {
	ref, err := ParseReference(in.App)
	if err != nil {
		return nil, DeployHookResponse{}, err
	}
	hook, err := t.svc.RotateDeployHook(ctx, t.caller, ref)
	if err != nil {
		return nil, DeployHookResponse{}, err
	}
	return nil, toDeployHookResponse(hook), nil
}
