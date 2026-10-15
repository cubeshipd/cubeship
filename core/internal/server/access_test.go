package server

import (
	"context"
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

// **A tool nobody classified is a tool a restricted caller is shown.**
// The list decides what an agent holding a narrow key can even see, so
// it has to be the whole list.
func TestEveryMCPToolIsClassified(t *testing.T) {
	names := registeredTools(t)
	if len(names) < 10 {
		t.Fatalf("only %d tools registered, so this proves nothing", len(names))
	}
	registered := map[string]bool{}
	for _, name := range names {
		registered[name] = true
		if _, ok := toolRules[name]; !ok {
			t.Errorf("tool %s is not in toolRules: say which resource it reaches and at what level", name)
		}
	}
	for name := range toolRules {
		if !registered[name] {
			t.Errorf("toolRules names %s, which no module registers", name)
		}
		if toolChanges(name) && !audit.Describes("mcp "+name) {
			t.Errorf("tool %s has no sentence in audit's tools, so the log would show its name", name)
		}
	}
}

func caller(grants ...user.Grant) *user.User {
	return &user.User{Username: "agent", Role: user.RoleMember, Policy: user.NewPolicy(grants)}
}

func TestACallerIsOfferedOnlyWhatItsGrantsReach(t *testing.T) {
	readOnly := caller(
		user.Grant{Resource: user.ResApps, Level: user.LevelView},
		user.Grant{Resource: user.ResDatabases, Level: user.LevelView},
	)
	for _, name := range []string{"list_apps", "get_app", "get_app_logs", "whoami", "get_datastore"} {
		if !toolAvailable(readOnly, name) {
			t.Errorf("a read-only caller lost %s", name)
		}
	}
	for _, name := range []string{"deploy_app", "get_app_env", "create_datastore", "list_servers", "install_template"} {
		if toolAvailable(readOnly, name) {
			t.Errorf("a read-only caller was offered %s", name)
		}
	}

	deployer := caller(user.Grant{Resource: user.ResApps, Level: user.LevelManage, Secrets: true, Items: []string{"web"}})
	for _, name := range []string{"deploy_app", "get_app_env", "set_app_env"} {
		if !toolAvailable(deployer, name) {
			t.Errorf("a caller managing web's apps lost %s", name)
		}
	}
	if toolAvailable(caller(user.Grant{Resource: user.ResApps, Level: user.LevelManage, Items: []string{}}), "deploy_app") {
		t.Error("a grant with no items left was offered deploy_app")
	}
}

func TestAnAdminHasEveryTool(t *testing.T) {
	admin := &user.User{Username: "admin", Role: user.RoleAdmin}
	for name := range toolRules {
		if !toolAvailable(admin, name) {
			t.Errorf("%s is missing for an admin", name)
		}
	}
}
