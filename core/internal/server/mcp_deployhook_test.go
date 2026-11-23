package server_test

import (
	"net/http"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type mcpDeployHook struct {
	URL   *string `json:"url"`
	Token string  `json:"token"`
}

// The webhook tools answer what the HTTP routes do, and refuse whom
// they refuse: a member reads the hook of an app that runs an image and
// not of one that builds.
func TestMCPDeployHookMatchesHTTP(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "image", "project": "web", "source": "external", "image": "ghcr.io/acme/image",
	}, memberKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "built", "project": "web", "source": "dockerfile", "repo": "https://github.com/acme/api.git",
	}, f.AdminKey), http.StatusCreated)
	const image, built = "web/production/image", "web/production/built"

	session := connectMCP(t, f, memberKey)
	viaMCP, result := callTool[mcpDeployHook](t, session, "get_app_deploy_hook", map[string]any{"app": image})
	if result.IsError {
		t.Fatalf("get_app_deploy_hook: %s", toolErrorText(result))
	}
	var viaHTTP mcpDeployHook
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+image+"/deploy-hook", nil, memberKey, &viaHTTP), http.StatusOK)
	if viaMCP.Token == "" || viaMCP.Token != viaHTTP.Token || viaMCP.URL == nil || viaHTTP.URL == nil || *viaMCP.URL != *viaHTTP.URL {
		t.Fatalf("MCP read %+v, HTTP read %+v", viaMCP, viaHTTP)
	}

	rotated, result := callTool[mcpDeployHook](t, session, "rotate_app_deploy_hook", map[string]any{"app": image})
	if result.IsError {
		t.Fatalf("rotate_app_deploy_hook: %s", toolErrorText(result))
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+image+"/deploy-hook", nil, memberKey, &viaHTTP), http.StatusOK)
	if rotated.Token == viaMCP.Token || rotated.Token != viaHTTP.Token {
		t.Errorf("MCP rotated to %q from %q; HTTP now reads %q", rotated.Token, viaMCP.Token, viaHTTP.Token)
	}

	for _, tool := range []string{"get_app_deploy_hook", "rotate_app_deploy_hook"} {
		if !refused(t, session, tool, map[string]any{"app": built}) {
			t.Errorf("a member ran %s on an app that builds", tool)
		}
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps/"+built+"/deploy-hook", nil, memberKey), http.StatusForbidden)
	if _, result := callTool[mcpDeployHook](t, connectMCP(t, f, f.AdminKey), "get_app_deploy_hook", map[string]any{"app": built}); result.IsError {
		t.Errorf("an admin was refused: %s", toolErrorText(result))
	}
}
