package server_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPredeployCommandSchema(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"create_app", "update_app"} {
		t.Run(name, func(t *testing.T) {
			for _, tool := range listed.Tools {
				if tool.Name != name {
					continue
				}
				raw, err := json.Marshal(tool.InputSchema)
				if err != nil {
					t.Fatal(err)
				}
				var schema struct {
					Properties map[string]json.RawMessage `json:"properties"`
				}
				if err := json.Unmarshal(raw, &schema); err != nil {
					t.Fatal(err)
				}
				var command struct {
					Types []string `json:"type"`
					Items *struct {
						Type string `json:"type"`
					} `json:"items"`
				}
				if err := json.Unmarshal(schema.Properties["predeploy_command"], &command); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(command.Types, []string{"string", "array", "null"}) || command.Items == nil || command.Items.Type != "string" {
					t.Fatalf("command schema must accept shell string and string argv: %s", schema.Properties["predeploy_command"])
				}
				return
			}
			t.Fatal("tool not listed")
		})
	}
}

func TestMCPPredeployCommandRoundTrip(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)
	invoke := func(t *testing.T, name string, args map[string]any) map[string]any {
		t.Helper()
		out, result := callTool[map[string]any](t, session, name, args)
		if result.IsError {
			t.Fatalf("%s: %s", name, toolErrorText(result))
		}
		return out
	}
	for _, tc := range []struct {
		name    string
		command any
	}{
		{"shell", "echo ready && migrate"}, {"argv", []string{"apolo-gateway", "wait-for-idle"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := "web/production/" + tc.name
			invoke(t, "create_app", map[string]any{"project": "web", "name": tc.name, "predeploy_command": tc.command, "predeploy_timeout": 42})
			assertCommand := func(want any) {
				t.Helper()
				out := invoke(t, "get_app", map[string]any{"app": ref})
				raw, _ := json.Marshal(want)
				var normalized any
				json.Unmarshal(raw, &normalized)
				if !reflect.DeepEqual(out["predeploy_command"], normalized) {
					t.Fatalf("command = %#v, want %#v", out["predeploy_command"], normalized)
				}
				if out["predeploy_timeout"] != float64(42) {
					t.Fatalf("timeout changed: %#v", out)
				}
			}
			assertCommand(tc.command)
			listed := invoke(t, "list_apps", nil)
			if len(listed["items"].([]any)) == 0 {
				t.Fatal("list_apps lost configured app")
			}
			invoke(t, "update_app", map[string]any{"reference": ref, "health_path": "/ready"})
			assertCommand(tc.command)
			for _, command := range []any{"echo updated", []string{"apolo-gateway", "wait-for-idle"}} {
				invoke(t, "update_app", map[string]any{"reference": ref, "predeploy_command": command})
				assertCommand(command)
			}
			current := invoke(t, "get_app", map[string]any{"app": ref})
			if current["health_path"] != "/ready" {
				t.Fatalf("omitted health_path changed: %#v", current)
			}
			for _, invalid := range []any{true, 7, map[string]any{"command": "echo"}, []any{"echo", 1}, []int{65, 66}} {
				for _, tool := range []string{"create_app", "update_app"} {
					args := map[string]any{"reference": ref, "predeploy_command": invalid}
					if tool == "create_app" {
						args = map[string]any{"project": "web", "name": "invalid", "predeploy_command": invalid}
					}
					result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
					if err == nil && !result.IsError {
						t.Fatalf("%s accepted invalid command %#v", tool, invalid)
					}
				}
				assertCommand([]string{"apolo-gateway", "wait-for-idle"})
			}
			for _, clear := range []any{"", []string{}, nil} {
				invoke(t, "update_app", map[string]any{"reference": ref, "predeploy_command": "echo reset"})
				invoke(t, "update_app", map[string]any{"reference": ref, "predeploy_command": clear})
				assertCommand(nil)
			}
		})
	}
}

func TestMCPClearsHealthPath(t *testing.T) {
	f := servertest.New(t)
	session := connectMCP(t, f, f.AdminKey)
	_, created := callTool[map[string]any](t, session, "create_app", map[string]any{"project": "web", "name": "health"})
	if created.IsError {
		t.Fatal(toolErrorText(created))
	}
	for _, path := range []string{"/healthz", ""} {
		_, result := callTool[map[string]any](t, session, "update_app", map[string]any{"reference": "web/production/health", "health_path": path})
		if result.IsError {
			t.Fatalf("health_path %q: %s", path, toolErrorText(result))
		}
		got, result := callTool[map[string]any](t, session, "get_app", map[string]any{"app": "web/production/health"})
		if result.IsError {
			t.Fatal(toolErrorText(result))
		}
		if path != "" {
			_, nullResult := callTool[map[string]any](t, session, "update_app", map[string]any{"reference": "web/production/health", "health_path": nil})
			if !nullResult.IsError || !strings.Contains(toolErrorText(nullResult), "nothing to change") {
				t.Fatalf("null health_path must mean omitted: %s", toolErrorText(nullResult))
			}
			unchanged, nullResult := callTool[map[string]any](t, session, "get_app", map[string]any{"app": "web/production/health"})
			if nullResult.IsError || unchanged["health_path"] != path {
				t.Fatalf("null changed health_path: %#v", unchanged)
			}
		}
		if path == "" {
			if _, exists := got["health_path"]; exists {
				t.Fatalf("cleared health_path should be omitted: %#v", got)
			}
		} else if got["health_path"] != path {
			t.Fatalf("health_path = %#v", got["health_path"])
		}
	}
}
