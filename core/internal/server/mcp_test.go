package server_test

import (
	"context"
	"cubeship/internal/user"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	t.Run("a member cannot create a project", func(t *testing.T) {
		session := connectMCP(t, f, memberKey)
		if !refused(t, session, "create_project", map[string]any{"slug": "globex"}) {
			t.Fatal("a member was allowed to create a project over MCP")
		}
	})

	t.Run("an admin can", func(t *testing.T) {
		session := connectMCP(t, f, f.AdminKey)
		out, result := callTool[struct {
			Slug string `json:"slug"`
		}](t, session, "create_project", map[string]any{"slug": "globex"})
		if result.IsError {
			t.Fatalf("admin create_project failed: %s", toolErrorText(result))
		}
		if out.Slug != "globex" {
			t.Errorf("got slug %q, want globex", out.Slug)
		}
	})

	t.Run("a member cannot create a project", func(t *testing.T) {
		session := connectMCP(t, f, memberKey)
		if !refused(t, session, "create_project", map[string]any{"slug": "nope"}) {
			t.Fatal("a member was allowed to create a project over MCP")
		}
	})

	t.Run("an outsider sees no apps", func(t *testing.T) {
		_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", user.RoleMember)
		session := connectMCP(t, f, outsiderKey)
		out, result := callTool[struct {
			Items []map[string]any `json:"items"`
		}](t, session, "list_apps", nil)
		if result.IsError {
			t.Fatalf("list_apps failed: %s", toolErrorText(result))
		}
		if len(out.Items) != 0 {
			t.Fatalf("an outsider saw apps: %v", out.Items)
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
	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	session := connectMCP(t, f, memberKey)

	created, result := callTool[struct {
		Reference   string `json:"reference"`
		Name        string `json:"name"`
		Image       string `json:"image"`
		Project     string `json:"project"`
		Environment string `json:"environment"`
	}](t, session, "create_app", map[string]any{
		"project": "web", "name": "myapp"})
	if result.IsError {
		t.Fatalf("create_app failed: %s", toolErrorText(result))
	}

	// The reference is the registry path minus the host — that identity
	// is the point of scoping names to their environment.
	if created.Reference != "web/production/myapp" {
		t.Errorf("reference is %q, want web/production/myapp", created.Reference)
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

	// The two-part shorthand names the production environment.
	var shorthand struct {
		Reference string `json:"reference"`
	}
	got, result := callTool[struct {
		Reference string `json:"reference"`
	}](t, session, "get_app", map[string]any{"app": "web/myapp"})
	if result.IsError {
		t.Fatalf("get_app with the shorthand failed: %s", toolErrorText(result))
	}
	shorthand = got
	if shorthand.Reference != created.Reference {
		t.Errorf("the shorthand resolved to %q, want %q", shorthand.Reference, created.Reference)
	}
}

// refused reports whether a tool call was turned away: an error result, or
// the tool not being offered to this caller at all, which MCP answers as an
// unknown tool.
func refused(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) bool {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	return err != nil || result.IsError
}

// A tool's output schema must be an object schema. A handler returning a
// Go slice infers {"type": ["null", "array"]} instead, and a client that
// validates tools/list against the MCP schema — pydantic-backed ones do —
// rejects the whole list over a single tool, so every tool disappears at
// once. mcpx.List is what keeps the results inside an object.
func TestMCPToolOutputSchemasAreObjects(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("no tools registered")
	}
	for _, tool := range tools.Tools {
		if tool.OutputSchema == nil {
			continue
		}
		raw, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("marshal %s output schema: %v", tool.Name, err)
		}
		var schema struct {
			Type any `json:"type"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s output schema: %v", tool.Name, err)
		}
		if schema.Type != "object" {
			t.Errorf("tool %s has output schema type %v, want object", tool.Name, schema.Type)
		}
	}
}

// The gap this closes: every other part of an app's configuration was
// reachable over MCP, so an agent could create a project, an app and a
// database and then had to stop and ask somebody to click the one
// setting that decides whether the app answers at all.
func TestMCPManagesAnAppsDomains(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)

	if _, result := callTool[struct{}](t, session, "create_app",
		map[string]any{"project": "web", "name": "myapp"}); result.IsError {
		t.Fatalf("create_app failed: %s", toolErrorText(result))
	}

	type domains struct {
		Domains []struct {
			ID   int64  `json:"id"`
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"domains"`
	}

	out, result := callTool[domains](t, session, "add_app_domain",
		map[string]any{"app": "web/myapp", "host": "apiv2.example.com", "port": 3000})
	if result.IsError {
		t.Fatalf("add_app_domain failed: %s", toolErrorText(result))
	}
	if len(out.Domains) != 1 || out.Domains[0].Host != "apiv2.example.com" || out.Domains[0].Port != 3000 {
		t.Fatalf("after adding, domains are %+v", out.Domains)
	}

	// The tool this issue is really about: the app listens on 3100 and
	// the name points at 3000, which is a healthy container behind a
	// proxy answering 502.
	out, result = callTool[domains](t, session, "set_app_domain_port",
		map[string]any{"app": "web/myapp", "host": "apiv2.example.com", "port": 3100})
	if result.IsError {
		t.Fatalf("set_app_domain_port failed: %s", toolErrorText(result))
	}
	if len(out.Domains) != 1 || out.Domains[0].Port != 3100 {
		t.Fatalf("after changing the port, domains are %+v", out.Domains)
	}

	// HTTP sees the same thing, which is the whole claim of a second
	// door onto one house.
	var viaHTTP struct {
		Domains []struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"domains"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/web/production/myapp", nil, f.AdminKey, &viaHTTP), http.StatusOK)
	if len(viaHTTP.Domains) != 1 || viaHTTP.Domains[0].Port != 3100 {
		t.Errorf("HTTP sees %+v, MCP set port 3100", viaHTTP.Domains)
	}

	// Naming a host the app does not have is refused, rather than
	// silently changing whichever domain happened to be first.
	if !refused(t, session, "set_app_domain_port",
		map[string]any{"app": "web/myapp", "host": "nope.example.com", "port": 3100}) {
		t.Error("a host the app does not answer at was accepted")
	}

	out, result = callTool[domains](t, session, "remove_app_domain",
		map[string]any{"app": "web/myapp", "host": "apiv2.example.com"})
	if result.IsError {
		t.Fatalf("remove_app_domain failed: %s", toolErrorText(result))
	}
	if len(out.Domains) != 0 {
		t.Errorf("after removing, domains are %+v", out.Domains)
	}
}
