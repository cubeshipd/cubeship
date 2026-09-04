package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/server/servertest"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerRoundTripper adds an Authorization: Bearer header to every
// outgoing request — the MCP client transport has no built-in notion of a
// static bearer token, only OAuth, so this stands in for the API key auth
// /mcp actually expects.
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

// connectMCP returns a real MCP client session against the fixture's real
// /mcp endpoint, authenticated as apiKey.
func connectMCP(t *testing.T, f *servertest.Fixture, apiKey string) *mcp.ClientSession {
	t.Helper()
	ts := f.HTTPServer(t)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: &bearerRoundTripper{token: apiKey}},
	}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
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

func TestMCPRejectsUnauthenticatedRequests(t *testing.T) {
	f := servertest.New(t)
	ts := f.HTTPServer(t)

	resp, err := http.Post(ts.URL+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// Every tool must be authorized exactly like the equivalent HTTP request.
// The MCP surface is a second door onto the same house, not a way around
// the locks.
func TestMCPToolsAreAuthorizedLikeHTTP(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	t.Run("a member cannot create an organization", func(t *testing.T) {
		session := connectMCP(t, f, memberKey)
		_, result := callTool[map[string]any](t, session, "create_org",
			map[string]any{"slug": "globex", "name": "Globex"})
		if !result.IsError {
			t.Fatal("a member was allowed to create an organization over MCP")
		}
	})

	t.Run("a super-admin can", func(t *testing.T) {
		session := connectMCP(t, f, f.AdminKey)
		out, result := callTool[struct {
			Slug string `json:"slug"`
		}](t, session, "create_org", map[string]any{"slug": "globex", "name": "Globex"})
		if result.IsError {
			t.Fatalf("super-admin create_org failed: %s", toolErrorText(result))
		}
		if out.Slug != "globex" {
			t.Errorf("got slug %q, want globex", out.Slug)
		}
	})

	t.Run("a member cannot create a project", func(t *testing.T) {
		session := connectMCP(t, f, memberKey)
		_, result := callTool[map[string]any](t, session, "create_project",
			map[string]any{"org": "acme", "slug": "nope", "name": "Nope"})
		if !result.IsError {
			t.Fatal("a member was allowed to create a project over MCP")
		}
	})

	t.Run("an outsider sees no apps", func(t *testing.T) {
		_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)
		session := connectMCP(t, f, outsiderKey)
		out, result := callTool[[]map[string]any](t, session, "list_apps", nil)
		if result.IsError {
			t.Fatalf("list_apps failed: %s", toolErrorText(result))
		}
		if len(out) != 0 {
			t.Fatalf("an outsider saw apps: %v", out)
		}
	})
}

// The whole reason a user can hold several keys: an agent's key and a
// terminal's key have to be independent, or "give the MCP client its own
// key" is meaningless advice.
func TestMCPCreatedKeyIsIndependentOfRotate(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)

	created, result := callTool[struct {
		APIKey string `json:"api_key"`
	}](t, session, "create_api_key", map[string]any{"name": "mcp"})
	if result.IsError {
		t.Fatalf("create_api_key failed: %s", toolErrorText(result))
	}
	if created.APIKey == "" {
		t.Fatal("create_api_key returned no key")
	}

	// Rotate the ORIGINAL key over HTTP, as a terminal would.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/users/me/api-key/rotate", nil, f.AdminKey), http.StatusOK)

	// The MCP key still works.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, created.APIKey), http.StatusOK)
}

// A tool that reaches an app must go through the same scope resolution
// the HTTP route does, including which project and environment it lands
// in.
func TestMCPCreateAppRoundTrip(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)
	session := connectMCP(t, f, memberKey)

	created, result := callTool[struct {
		Reference   string `json:"reference"`
		Name        string `json:"name"`
		Image       string `json:"image"`
		Org         string `json:"org"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
	}](t, session, "create_app", map[string]any{
		"org": "acme", "project": "web", "name": "myapp", "domain": "myapp.example.com",
	})
	if result.IsError {
		t.Fatalf("create_app failed: %s", toolErrorText(result))
	}

	// The reference is the registry path minus the host — that identity
	// is the point of scoping names to their environment.
	if created.Reference != "acme/web/production/myapp" {
		t.Errorf("reference is %q, want acme/web/production/myapp", created.Reference)
	}
	if created.Image != servertest.RegistryHost+"/"+created.Reference {
		t.Errorf("push path is %q, want %s/%s", created.Image, servertest.RegistryHost, created.Reference)
	}

	// And the same app is visible over HTTP, at the same reference.
	var viaHTTP struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+created.Reference, nil, memberKey, &viaHTTP), http.StatusOK)
	if viaHTTP.Reference != created.Reference {
		t.Errorf("HTTP sees %q, MCP created %q", viaHTTP.Reference, created.Reference)
	}

	// The three-part shorthand names the production environment.
	var shorthand struct {
		Reference string `json:"reference"`
	}
	got, result := callTool[struct {
		Reference string `json:"reference"`
	}](t, session, "get_app", map[string]any{"app": "acme/web/myapp"})
	if result.IsError {
		t.Fatalf("get_app with the shorthand failed: %s", toolErrorText(result))
	}
	shorthand = got
	if shorthand.Reference != created.Reference {
		t.Errorf("the shorthand resolved to %q, want %q", shorthand.Reference, created.Reference)
	}
}
