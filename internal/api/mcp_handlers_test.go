package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRoundTripper adds an Authorization: Bearer header to every
// outgoing request — the MCP client transport has no built-in notion of
// a static bearer token, only OAuth, so this stands in for the API key
// auth /mcp actually expects.
type bearerRoundTripper struct {
	token string
}

func (t *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// connectMCP starts an httptest.Server for srv and returns a connected
// MCP client session authenticated as apiKey (empty for unauthenticated).
// The caller must Close the session and the returned httptest.Server.
func connectMCP(t *testing.T, srv *Server, apiKey string) (*mcp.ClientSession, *httptest.Server) {
	t.Helper()
	httpServer := httptest.NewServer(srv.Router())

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   httpServer.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{token: apiKey}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		httpServer.Close()
		t.Fatalf("connect MCP client: %v", err)
	}
	return session, httpServer
}

func callTool[Out any](t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (Out, *mcp.CallToolResult) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	var out Out
	if !result.IsError && len(result.Content) > 0 {
		if tc, ok := result.Content[0].(*mcp.TextContent); ok {
			json.Unmarshal([]byte(tc.Text), &out)
		}
	}
	return out, result
}

func toolErrorText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

func TestMCPRejectsUnauthenticatedRequest(t *testing.T) {
	srv, _, _ := newTestServer(t)
	httpServer := httptest.NewServer(srv.Router())
	defer httpServer.Close()

	resp, err := http.Post(httpServer.URL+"/mcp", "application/json",
		nil)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMCPWhoAmI(t *testing.T) {
	srv, key, _ := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	out, result := callTool[whoAmIOutput](t, session, "whoami", map[string]any{})
	if result.IsError {
		t.Fatalf("whoami returned an error: %s", toolErrorText(result))
	}
	if out.Username != "test-admin" || !out.IsSuperAdmin {
		t.Fatalf("unexpected whoami output: %+v", out)
	}
}

func TestMCPCreateOrgRequiresSuperAdmin(t *testing.T) {
	srv, _, _ := newTestServer(t)
	memberKey := testAPIKeyFor(t, srv.store, false)
	session, httpServer := connectMCP(t, srv, memberKey)
	defer httpServer.Close()
	defer session.Close()

	_, result := callTool[orgResponse](t, session, "create_org", map[string]any{"slug": "globex", "name": "Globex Corp"})
	if !result.IsError {
		t.Fatal("expected create_org to fail for a non-super-admin")
	}
}

func TestMCPCreateOrgAsSuperAdmin(t *testing.T) {
	srv, key, _ := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	out, result := callTool[orgResponse](t, session, "create_org", map[string]any{"slug": "globex", "name": "Globex Corp"})
	if result.IsError {
		t.Fatalf("create_org failed: %s", toolErrorText(result))
	}
	if out.Slug != "globex" || out.Name != "Globex Corp" {
		t.Fatalf("unexpected org: %+v", out)
	}
}

// End-to-end resource creation through MCP: project -> app, then confirm
// it shows up in list_apps and get_app — the same round trip the HTTP
// API and CLI give you, just over MCP tools instead.
func TestMCPCreateProjectAndAppRoundTrip(t *testing.T) {
	srv, key, org := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	project, result := callTool[projectResponse](t, session, "create_project", map[string]any{
		"org": org.Slug, "slug": "web", "name": "Web",
	})
	if result.IsError {
		t.Fatalf("create_project failed: %s", toolErrorText(result))
	}
	if project.Slug != "web" || len(project.Environments) != 1 || project.Environments[0] != "production" {
		t.Fatalf("unexpected project: %+v", project)
	}

	app, result := callTool[appResponse](t, session, "create_app", map[string]any{
		"org": org.Slug, "project": "web", "name": "myapp", "domain": "myapp.example.com",
	})
	if result.IsError {
		t.Fatalf("create_app failed: %s", toolErrorText(result))
	}
	if app.Name != "myapp" || app.Environment != "production" || app.Image != "registry.example.com/acme/myapp" {
		t.Fatalf("unexpected app: %+v", app)
	}

	apps, result := callTool[[]appResponse](t, session, "list_apps", map[string]any{})
	if result.IsError {
		t.Fatalf("list_apps failed: %s", toolErrorText(result))
	}
	if len(apps) != 1 || apps[0].Name != "myapp" {
		t.Fatalf("unexpected apps list: %+v", apps)
	}

	got, result := callTool[appResponse](t, session, "get_app", map[string]any{"name": "myapp"})
	if result.IsError {
		t.Fatalf("get_app failed: %s", toolErrorText(result))
	}
	if got.Name != "myapp" {
		t.Fatalf("unexpected app: %+v", got)
	}
}

func TestMCPSetAppEnv(t *testing.T) {
	srv, key, org := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	callTool[appResponse](t, session, "create_app", map[string]any{
		"org": org.Slug, "project": testProjectSlug, "name": "myapp", "domain": "myapp.example.com",
	})
	_, result := callTool[actionResult](t, session, "set_app_env", map[string]any{
		"name": "myapp", "vars": map[string]any{"PORT": "8080"},
	})
	if result.IsError {
		t.Fatalf("set_app_env failed: %s", toolErrorText(result))
	}

	app, err := srv.store.GetAppByName(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if app.Env["PORT"] != "8080" {
		t.Fatalf("unexpected env: %v", app.Env)
	}
}

// An "mcp" key created via create_api_key must be independent of the
// key the session is already using — rotating the calling key must
// never revoke it. This is the entire point of named, multi-key
// credentials: an MCP client's own key survives routine rotation of a
// human's CLI key, and vice versa.
func TestMCPCreateAPIKeyIsIndependentOfRotate(t *testing.T) {
	srv, key, _ := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	created, result := callTool[createAPIKeyOutput](t, session, "create_api_key", map[string]any{"name": "mcp"})
	if result.IsError {
		t.Fatalf("create_api_key failed: %s", toolErrorText(result))
	}
	if created.APIKey == "" || created.Name != "mcp" {
		t.Fatalf("unexpected created key: %+v", created)
	}

	rotated, result := callTool[rotateMyAPIKeyOutput](t, session, "rotate_my_api_key", map[string]any{})
	if result.IsError {
		t.Fatalf("rotate_my_api_key failed: %s", toolErrorText(result))
	}
	if rotated.APIKey == "" || rotated.APIKey == key {
		t.Fatalf("expected a new, different key, got %q", rotated.APIKey)
	}

	// The independently created "mcp" key must still work, even though
	// the session's original key was just rotated away.
	mcpSession, mcpServer := connectMCP(t, srv, created.APIKey)
	defer mcpServer.Close()
	defer mcpSession.Close()
	out, result := callTool[whoAmIOutput](t, mcpSession, "whoami", map[string]any{})
	if result.IsError {
		t.Fatalf("whoami with the mcp key failed: %s", toolErrorText(result))
	}
	if out.Username != "test-admin" {
		t.Fatalf("unexpected whoami output: %+v", out)
	}
}

func TestMCPRevokeAPIKeyRefusesLastKey(t *testing.T) {
	srv, key, _ := newTestServer(t)
	session, httpServer := connectMCP(t, srv, key)
	defer httpServer.Close()
	defer session.Close()

	keys, result := callTool[[]apiKeyResponse](t, session, "list_api_keys", map[string]any{})
	if result.IsError {
		t.Fatalf("list_api_keys failed: %s", toolErrorText(result))
	}
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 key, got %d", len(keys))
	}

	_, result = callTool[actionResult](t, session, "revoke_api_key", map[string]any{"id": keys[0].ID})
	if !result.IsError {
		t.Fatal("expected revoking the only remaining key to fail")
	}
}
