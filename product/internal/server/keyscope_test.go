package server_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/audit"
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

type roleOut struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// seededRole is one of the roles the migration creates.
func seededRole(t *testing.T, f *servertest.Fixture, name string) int64 {
	t.Helper()
	var out struct {
		Roles []roleOut `json:"roles"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/roles", nil, f.AdminKey, &out), http.StatusOK)
	for _, r := range out.Roles {
		if r.Name == name {
			return r.ID
		}
	}
	t.Fatalf("no role %q among %+v", name, out.Roles)
	return 0
}

func createRole(t *testing.T, f *servertest.Fixture, name string, grants ...user.Grant) int64 {
	t.Helper()
	var out roleOut
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/roles",
		map[string]any{"name": name, "grants": grants}, f.AdminKey, &out), http.StatusCreated)
	return out.ID
}

// Every change the API can record reads as a sentence.
func TestEveryRecordedRouteHasASentence(t *testing.T) {
	f := servertest.New(t)
	// Not behind authentication, so never recorded.
	unrecorded := map[string]bool{
		"POST /setup": true, "POST /auth/login": true, "POST /mcp": true,
		"POST /hooks/github": true, "POST /hooks/registry": true,
		"POST /nodes/agent/reconcile": true, "POST /nodes/agent/results/{id}": true,
	}
	for _, p := range append(f.Server.Patterns(), f.Server.InternalPatterns()...) {
		method, _, found := strings.Cut(p, " ")
		if !found || method == "GET" || method == "HEAD" || unrecorded[p] {
			continue
		}
		if !audit.Describes(p) {
			t.Errorf("route %s has no sentence in audit's routes", p)
		}
	}
}

func TestARoleHoldsAMemberToItsGrants(t *testing.T) {
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "shop"}, f.AdminKey), http.StatusCreated)
	role := createRole(t, f, "web deployer",
		user.Grant{Resource: user.ResApps, Level: user.LevelManage, Secrets: true, Items: []string{"web"}})
	_, key := f.AddMember(t, "dev", user.RoleMember)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/dev", map[string]any{"access_role_id": role}, f.AdminKey), http.StatusOK)

	var projects []struct {
		Slug string `json:"slug"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/projects", nil, key, &projects), http.StatusOK)
	if len(projects) != 1 || projects[0].Slug != "web" {
		t.Fatalf("a member given web's apps listed %v", projects)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "shop", "name": "api"}, key), http.StatusNotFound)
	if rec := f.Do(t, http.MethodPost, "/apps", map[string]any{"project": "web", "name": "api"}, key); rec.Code >= 300 {
		t.Fatalf("a member managing web's apps could not create one: %d %s", rec.Code, rec.Body)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "nope"}, key), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/datastores", map[string]any{"slug": "db", "engine": "postgres"}, key), http.StatusForbidden)

	// Back to the member default, which reads the databases again.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/dev", map[string]any{"access_role_id": 0}, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/shop/environments", nil, key), http.StatusOK)
}

func TestAKeyRoleNarrowsItsOwner(t *testing.T) {
	f := servertest.New(t)
	readOnly := seededRole(t, f, "Read only")
	deploy := seededRole(t, f, "Deploy")
	read := issueKey(t, f, f.AdminKey, map[string]any{"name": "watcher", "access_role_id": readOnly}, http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/apps", nil, read), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "nope"}, read), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects/web/env", nil, read), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users", nil, read), http.StatusForbidden)

	// It cannot hand out more than it has, cannot revoke, and a key asked
	// for by name alone carries its role.
	issueKey(t, f, read, map[string]any{"name": "wider", "access_role_id": deploy}, http.StatusForbidden)
	issueKey(t, f, read, map[string]any{"name": "same"}, http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/me/api-keys/1", nil, read), http.StatusForbidden)

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
		t.Error("a read-only key lost list_apps")
	}
	for _, hidden := range []string{"deploy_app", "get_app_env", "create_project"} {
		if slices.Contains(names, hidden) {
			t.Errorf("a read-only key was offered %s", hidden)
		}
	}
	if _, result := callTool[map[string]any](t, session, "create_project", map[string]any{"slug": "nope"}); !result.IsError {
		t.Fatal("a read-only key created a project over MCP")
	}
}

func TestRolesAreAnAdminsToChange(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "dev", user.RoleMember)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/roles", map[string]any{"name": "mine", "grants": []any{}}, key), http.StatusForbidden)

	role := createRole(t, f, "ops", user.Grant{Resource: user.ResServers, Level: user.LevelView})
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/dev", map[string]any{"access_role_id": role}, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/roles/"+strconv.FormatInt(role, 10), nil, f.AdminKey), http.StatusConflict)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/roles",
		map[string]any{"name": "bad", "grants": []map[string]any{{"resource": "users", "level": "view", "items": nil}}}, f.AdminKey), http.StatusBadRequest)

	// The roles the instance ships mean what they say for everybody
	// holding them, so nobody changes or deletes one.
	deploy := strconv.FormatInt(seededRole(t, f, "Deploy"), 10)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/roles/"+deploy,
		map[string]any{"name": "Deploy", "grants": []any{}}, f.AdminKey), http.StatusConflict)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/roles/"+deploy, nil, f.AdminKey), http.StatusConflict)
}

type auditPage struct {
	Events []struct {
		Username string `json:"username"`
		Via      string `json:"via"`
		Action   string `json:"action"`
		Target   string `json:"target"`
		Outcome  string `json:"outcome"`
	} `json:"events"`
}

func TestChangesAndRefusalsAreAudited(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	read := issueKey(t, f, f.AdminKey, map[string]any{"name": "watcher", "access_role_id": seededRole(t, f, "Read only")}, http.StatusCreated)

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
		t.Errorf("a read-only key's refused write is not in the log: %+v", page.Events)
	}
	if !has("mcp create_app", "ok", "mcp") {
		t.Errorf("an app created over MCP is not in the log: %+v", page.Events)
	}
	for _, e := range page.Events {
		if e.Action == "GET /apps" {
			t.Errorf("a read that was allowed was recorded: %+v", e)
		}
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/audit", nil, memberKey), http.StatusForbidden)
}
