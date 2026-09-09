package app_test

import (
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
)

type placedApp struct {
	Reference string `json:"reference"`
	Node      string `json:"node"`
	Source    string `json:"source"`
	Address   string `json:"address"`
}

// addServer puts a machine in the cluster. Nothing is contacted — the
// row is a place for a box that does not exist — which is exactly what
// these tests want: the placement decisions are the control plane's,
// and none of them wait for a machine to answer.
// addServer puts a machine in the cluster and hands back the credential
// its agent authenticates with, which is shown once and only here.
func addServer(t *testing.T, f *servertest.Fixture, name string) string {
	t.Helper()
	var created struct {
		Token string `json:"token"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/nodes",
		map[string]any{"name": name}, f.AdminKey, &created), http.StatusCreated)
	return created.Token
}

func createExternalApp(t *testing.T, f *servertest.Fixture, name string) placedApp {
	t.Helper()
	var created placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": "web", "source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey, &created), http.StatusCreated)
	return created
}

// Every app runs somewhere, and on an instance of one box that is the
// box. It is a fact written down rather than a default nobody set:
// everything that exists today runs where the daemon does, because
// until there was a cluster there was nowhere else.
func TestAnAppIsOnTheControlPlaneUntilItIsMoved(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "api")
	if created.Node != node.ControlPlaneSlug {
		t.Errorf("a new app is on %q, want the control plane", created.Node)
	}

	_ = addServer(t, f, "eu-1")
	var moved placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey, &moved), http.StatusOK)
	if moved.Node != "eu-1" {
		t.Errorf("after moving it, the app is on %q", moved.Node)
	}

	// And back, which is always allowed: the control plane is where
	// everything works.
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": node.ControlPlaneSlug}, f.AdminKey, &moved), http.StatusOK)
	if moved.Node != node.ControlPlaneSlug {
		t.Errorf("the app came back to %q", moved.Node)
	}
}

// An app with a name to answer at can move, and the name goes with it:
// every machine runs its own edge, so the app is served wherever it is.
//
// What does not follow on its own is the DNS record — which is why the
// app says where its traffic has to arrive rather than the instance
// saying it once for everything.
func TestAnAppWithADomainCanMoveAndSaysWhereItsTrafficGoes(t *testing.T) {
	f := servertest.New(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)

	var moved placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey, &moved), http.StatusOK)
	if moved.Node != "eu-1" {
		t.Fatalf("the app is on %q", moved.Node)
	}
	// The machine has never called in, so it has no address to report —
	// and an app whose machine has no address has nothing to point a
	// name at. Saying the instance's own would be a record reaching the
	// box the app just left.
	if moved.Address != "" {
		t.Errorf("address = %q, want none until that machine reports one", moved.Address)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes/agent/reconcile",
		node.AgentRequest{Cores: 2, Address: "203.0.113.9"}, token), http.StatusOK)

	var after placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference, nil, f.AdminKey, &after), http.StatusOK)
	if after.Address != "203.0.113.9" {
		t.Errorf("address = %q, want the machine the app is on", after.Address)
	}
}

// An app built here cannot run there. The image is loaded into this
// machine's Docker rather than pushed anywhere, so another machine has
// nowhere to pull it from — and a placement that cannot be pulled is a
// deploy that fails on a box nobody is looking at.
func TestAnAppBuiltHereCannotRunElsewhereYet(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")

	var created placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]any{
		"name": "worker", "project": "web", "source": "railpack",
		"repo": "https://github.com/acme/worker",
	}, f.AdminKey, &created), http.StatusCreated)

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("moving an app that builds: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "registry") {
		t.Errorf("the refusal is %q, and it has to say what would fix it", rec.Body.String())
	}
}

// A machine that is not in the cluster is not somewhere to run
// anything. Refused by name rather than written as a null the column
// would reject with a message nobody can read.
func TestAnAppCannotBePlacedOnAMachineThatIsNotHere(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "api")

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "somewhere-else"}, f.AdminKey)
	if rec.Code != http.StatusNotFound {
		t.Errorf("placing an app on a machine that is not here: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// A machine with apps on it cannot be removed. Where those apps should
// go is a decision, and making it by deleting a row would make it
// invisibly — the app would be pointing at a machine that is not there.
func TestAMachineWithAppsOnItCannotJustGo(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey), http.StatusOK)

	rec := f.Do(t, http.MethodDelete, "/nodes/eu-1", nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("removing a machine with an app on it: %d %s, want 409", rec.Code, rec.Body.String())
	}

	// Moved off, it goes.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": node.ControlPlaneSlug}, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/nodes/eu-1", nil, f.AdminKey), http.StatusOK)
}
