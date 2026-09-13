package server_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"cubeship/internal/audit"
	"cubeship/internal/server"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

func issueKey(t *testing.T, f *servertest.Fixture, withKey string, body map[string]any, want int) string {
	t.Helper()
	var out struct {
		APIKey string `json:"api_key"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/users/me/api-keys", body, withKey, &out), want)
	return out.APIKey
}

func TestEveryRouteTheKeyPolicyNamesExists(t *testing.T) {
	f := servertest.New(t)
	served := map[string]bool{}
	for _, p := range append(f.Server.Patterns(), f.Server.InternalPatterns()...) {
		served[p] = true
	}
	for _, list := range []map[string]bool{server.SecretRoutes, server.ScopedRoutes} {
		for pattern := range list {
			if !served[pattern] {
				t.Errorf("the key policy names %s, which is not a route", pattern)
			}
		}
	}
}

// Every change the API can record reads as a sentence, and so does every
// secret a key can be refused.
func TestEveryRecordedRouteHasASentence(t *testing.T) {
	f := servertest.New(t)
	// Not behind authentication, so never recorded.
	unrecorded := map[string]bool{
		"POST /setup": true, "POST /auth/login": true, "POST /mcp": true,
		"POST /hooks/github": true, "POST /hooks/registry": true,
		"POST /nodes/agent/reconcile": true, "POST /nodes/agent/results/{id}": true,
	}
	for _, p := range append(f.Server.Patterns(), f.Server.InternalPatterns()...) {
		method, _ := strings.CutSuffix(strings.SplitN(p, " ", 2)[0], "")
		if method == "GET" || method == "HEAD" || !strings.Contains(p, " ") || unrecorded[p] {
			continue
		}
		if !audit.Describes(p) {
			t.Errorf("route %s has no sentence in audit's routes", p)
		}
	}
	for p := range server.SecretRoutes {
		if !audit.Describes(p) {
			t.Errorf("secret route %s has no sentence in audit's routes", p)
		}
	}
}

func TestAProjectKeyReachesOnlyItsProject(t *testing.T) {
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "shop"}, f.AdminKey), http.StatusCreated)
	agent := issueKey(t, f, f.AdminKey, map[string]any{"name": "agent", "access": "deploy", "projects": []string{"web"}}, http.StatusCreated)

	var projects []struct {
		Slug string `json:"slug"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/projects", nil, agent, &projects), http.StatusOK)
	if len(projects) != 1 || projects[0].Slug != "web" {
		t.Fatalf("a key held to web listed %v", projects)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/shop/environments", nil, agent), http.StatusNotFound)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "shop", "name": "api"}, agent), http.StatusNotFound)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/datastores", nil, agent), http.StatusForbidden)
	if rec := f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "web", "name": "api"}, agent); rec.Code >= 300 {
		t.Fatalf("a key held to web could not create an app in it: %d %s", rec.Code, rec.Body)
	}

	// Its own key cannot be widened, and one asked for by name alone is
	// no wider than it.
	issueKey(t, f, agent, map[string]any{"name": "wider", "projects": []string{"shop"}}, http.StatusForbidden)
	issueKey(t, f, agent, map[string]any{"name": "full", "access": "full"}, http.StatusForbidden)
	issueKey(t, f, agent, map[string]any{"name": "same"}, http.StatusCreated)
	var keys []struct {
		Name     string   `json:"name"`
		Access   string   `json:"access"`
		Projects []string `json:"projects"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/users/me/api-keys", nil, agent, &keys), http.StatusOK)
	i := slices.IndexFunc(keys, func(k struct {
		Name     string   `json:"name"`
		Access   string   `json:"access"`
		Projects []string `json:"projects"`
	}) bool {
		return k.Name == "same"
	})
	if i < 0 || keys[i].Access != "deploy" || !slices.Equal(keys[i].Projects, []string{"web"}) {
		t.Fatalf("a key made by a restricted key is %+v", keys)
	}

	// Nor can it revoke its owner's keys.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/me/api-keys/1", nil, agent), http.StatusForbidden)

	session := connectMCP(t, f, agent)
	listed, result := callTool[[]struct {
		Slug string `json:"slug"`
	}](t, session, "list_projects", nil)
	if result.IsError || len(listed) != 1 || listed[0].Slug != "web" {
		t.Fatalf("over MCP a key held to web listed %v (%s)", listed, toolErrorText(result))
	}
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name == "create_datastore" || tool.Name == "list_datastores" {
			t.Errorf("a key held to a project was offered %s", tool.Name)
		}
	}
}

func TestADeployKeyDoesNotCarryItsOwnersAdminRole(t *testing.T) {
	f := servertest.New(t)
	deploy := issueKey(t, f, f.AdminKey, map[string]any{"name": "ci", "access": "deploy"}, http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "nope"}, deploy), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "yes"}, f.AdminKey), http.StatusCreated)
}

func TestAReadKeyReadsAndChangesNothing(t *testing.T) {
	f := servertest.New(t)
	read := issueKey(t, f, f.AdminKey, map[string]any{"name": "watcher", "access": "read"}, http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps", nil, read), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "web", "name": "api"}, read), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/web/env", nil, read), http.StatusForbidden)

	session := connectMCP(t, f, read)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !slices.Contains(names, "list_apps") {
		t.Error("a read key lost list_apps")
	}
	for _, hidden := range []string{"deploy_app", "get_app_env", "create_project"} {
		if slices.Contains(names, hidden) {
			t.Errorf("a read key was offered %s", hidden)
		}
	}
	_, result := callTool[map[string]any](t, session, "create_project", map[string]any{"slug": "nope"})
	if !result.IsError {
		t.Fatal("a read key created a project over MCP")
	}
}

type auditPage struct {
	Events []struct {
		Username string `json:"username"`
		Via      string `json:"via"`
		KeyName  string `json:"key_name"`
		Action   string `json:"action"`
		Target   string `json:"target"`
		Outcome  string `json:"outcome"`
	} `json:"events"`
}

func TestChangesAndRefusalsAreAudited(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	read := issueKey(t, f, f.AdminKey, map[string]any{"name": "watcher", "access": "read"}, http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "shop"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps", nil, read), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "web", "name": "api"}, read), http.StatusForbidden)

	session := connectMCP(t, f, memberKey)
	if _, result := callTool[map[string]any](t, session, "create_app", map[string]any{"project": "web", "name": "viamcp"}); result.IsError {
		t.Fatalf("create_app: %s", toolErrorText(result))
	}

	var page auditPage
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/audit", nil, f.AdminKey, &page), http.StatusOK)

	has := func(action, outcome, via string) bool {
		for _, e := range page.Events {
			if e.Action == action && e.Outcome == outcome && e.Via == via {
				return true
			}
		}
		return false
	}
	if !has("POST /projects", "ok", "api") {
		t.Errorf("creating a project is not in the log: %+v", page.Events)
	}
	if !has("POST /apps", "refused", "api") {
		t.Errorf("a read key's refused write is not in the log: %+v", page.Events)
	}
	if !has("mcp create_app", "ok", "mcp") {
		t.Errorf("an app created over MCP is not in the log: %+v", page.Events)
	}
	for _, e := range page.Events {
		if e.Action == "GET /apps" {
			t.Errorf("a read that was allowed was recorded: %+v", e)
		}
		if e.Action == "mcp create_app" && !strings.Contains(e.Target, "name=viamcp") {
			t.Errorf("the MCP event does not say what it created: %q", e.Target)
		}
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/audit", nil, memberKey), http.StatusForbidden)
}
