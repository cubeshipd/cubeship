package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"cubeship/internal/audit"
	"cubeship/internal/user"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registeredTools lists what every module registers, asked the way a
// client asks. Services are nil: registering a tool calls none.
func registeredTools(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	srv := (&Server{}).registerMCPTools(&user.User{Username: "admin", Role: user.RoleAdmin}, "")
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

// **A tool nobody classified is a tool a restricted key might keep.** The
// list is what decides what an agent holding a read-only or a project key
// can even see, so it has to be the whole list.
func TestEveryMCPToolIsClassified(t *testing.T) {
	names := registeredTools(t)
	if len(names) < 10 {
		t.Fatalf("only %d tools registered, so this proves nothing", len(names))
	}
	registered := map[string]bool{}
	for _, name := range names {
		registered[name] = true
		if _, ok := toolRules[name]; !ok {
			t.Errorf("tool %s is not in toolRules: say whether it reads, reads a secret or changes something, and whether a project key keeps it", name)
		}
	}
	for name, rule := range toolRules {
		if !registered[name] {
			t.Errorf("toolRules names %s, which no module registers", name)
		}
		if rule.kind != toolRead && !audit.Describes("mcp "+name) {
			t.Errorf("tool %s has no sentence in audit's tools, so the log would show its name", name)
		}
	}
}

func key(access user.Access, projects ...string) *user.User {
	k := &user.KeyScope{Name: "agent", Access: access}
	if projects != nil {
		k.Projects = projects
	}
	return &user.User{Username: "lucas", Role: user.RoleMember, Key: k}
}

func TestAReadKeyHasOnlyTheToolsThatRead(t *testing.T) {
	read := key(user.AccessRead)
	for _, tool := range []string{"list_apps", "get_app", "get_app_logs", "whoami"} {
		if !toolAvailable(read, tool) {
			t.Errorf("a read key lost %s", tool)
		}
	}
	for _, tool := range []string{"deploy_app", "get_app_env", "get_project_env", "create_api_key", "delete_app"} {
		if toolAvailable(read, tool) {
			t.Errorf("a read key has %s", tool)
		}
	}
}

func TestAProjectKeyHasNothingThatBelongsToTheInstance(t *testing.T) {
	scoped := key(user.AccessFull, "web")
	for _, tool := range []string{"create_datastore", "list_datastores", "install_template", "create_project", "list_audit_events"} {
		if toolAvailable(scoped, tool) {
			t.Errorf("a project key has %s", tool)
		}
	}
	for _, tool := range []string{"list_projects", "deploy_app", "get_app_env", "create_app"} {
		if !toolAvailable(scoped, tool) {
			t.Errorf("a project key lost %s", tool)
		}
	}
}

func TestASessionAndAFullKeyHaveEveryTool(t *testing.T) {
	for _, caller := range []*user.User{{Username: "lucas"}, key(user.AccessFull)} {
		for name := range toolRules {
			if !toolAvailable(caller, name) {
				t.Errorf("%s is missing for an unrestricted caller", name)
			}
		}
	}
}

func route(method, pattern string, values map[string]string) *http.Request {
	r := httptest.NewRequest(method, "/", nil)
	r.Pattern = pattern
	for k, v := range values {
		r.SetPathValue(k, v)
	}
	return r
}

func TestTheKeyPolicyAtTheHTTPDoor(t *testing.T) {
	cases := []struct {
		name    string
		caller  *user.User
		method  string
		pattern string
		values  map[string]string
		want    int
	}{
		{"a read key reads", key(user.AccessRead), "GET", "GET /apps", nil, 0},
		{"a read key writes nothing", key(user.AccessRead), "POST", "POST /apps", nil, 403},
		{"a read key reads no variables", key(user.AccessRead), "GET", "GET /apps/{project}/{env}/{name}/env",
			map[string]string{"project": "web"}, 403},
		{"a read key downloads no backup", key(user.AccessRead), "GET", "GET /backups/{id}/download", nil, 403},
		{"a deploy key writes", key(user.AccessDeploy), "POST", "POST /apps", nil, 0},
		{"a project key reaches its project", key(user.AccessFull, "web"), "POST", "POST /apps/{project}/{env}/{name}/deployments",
			map[string]string{"project": "web"}, 0},
		{"another project is not there", key(user.AccessFull, "web"), "GET", "GET /projects/{projectSlug}/environments",
			map[string]string{"projectSlug": "shop"}, 404},
		{"the instance's databases are not a project's", key(user.AccessFull, "web"), "GET", "GET /datastores", nil, 403},
		{"a project key lists projects", key(user.AccessFull, "web"), "GET", "GET /projects", nil, 0},
		{"a project key with none left reaches none", key(user.AccessFull), "GET", "GET /projects/{projectSlug}/env",
			map[string]string{"projectSlug": "web"}, 0},
	}
	cases[len(cases)-1].caller.Key.Projects = []string{}
	cases[len(cases)-1].want = 404

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, err := allowRoute(c.caller, route(c.method, c.pattern, c.values), c.pattern)
			if err == nil {
				status = 0
			}
			if status != c.want {
				t.Errorf("status %d (%v), want %d", status, err, c.want)
			}
		})
	}
}
