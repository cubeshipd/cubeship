package app_test

import (
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
)

type placedApp struct {
	Reference string   `json:"reference"`
	Nodes     []string `json:"nodes"`
	Status    string   `json:"status"`
	Source    string   `json:"source"`
	Address   string   `json:"address"`
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
	if len(created.Nodes) != 1 || created.Nodes[0] != node.ControlPlaneSlug {
		t.Errorf("a new app runs on %v, want the control plane alone", created.Nodes)
	}

	_ = addServer(t, f, "eu-1")
	var moved placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey, &moved), http.StatusOK)
	if len(moved.Nodes) != 1 || moved.Nodes[0] != "eu-1" {
		t.Errorf("after moving it, the app runs on %v", moved.Nodes)
	}

	// And back, which is always allowed: the control plane is where
	// everything works.
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": node.ControlPlaneSlug}, f.AdminKey, &moved), http.StatusOK)
	if len(moved.Nodes) != 1 || moved.Nodes[0] != node.ControlPlaneSlug {
		t.Errorf("the app came back to %v", moved.Nodes)
	}
}

// An app with a name to answer at can move, and **the record does not
// move with it**. That is the whole of what one front door buys: every
// name arrives at the control plane, which routes it to whichever
// machine runs the app, so moving an app is not a DNS change and not a
// second certificate.
func TestMovingAnAppDoesNotMoveWhereItsTrafficArrives(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)

	var before placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference,
		nil, f.AdminKey, &before), http.StatusOK)

	var moved placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey, &moved), http.StatusOK)
	if len(moved.Nodes) != 1 || moved.Nodes[0] != "eu-1" {
		t.Fatalf("the app runs on %v", moved.Nodes)
	}
	if moved.Address != before.Address {
		t.Errorf("the record moved from %q to %q; nothing about where an app runs should touch DNS",
			before.Address, moved.Address)
	}
}

func createBuildingApp(t *testing.T, f *servertest.Fixture, name string) placedApp {
	t.Helper()
	var created placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": "web", "source": "railpack",
		"repo": "https://github.com/acme/" + name,
	}, f.AdminKey, &created), http.StatusCreated)
	return created
}

// An app built here can run there. The build still happens on the
// control plane — that is where the builder and the repository
// credentials are — and its result is pushed to this instance's own
// registry instead of being loaded into this machine's Engine, which is
// the one address every machine in the cluster can pull from.
func TestAnAppThatBuildsCanBePlacedOnceThereIsARegistry(t *testing.T) {
	f := servertest.New(t)
	_ = addServer(t, f, "eu-1")
	created := createBuildingApp(t, f, "worker")

	var moved placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey, &moved), http.StatusOK)
	if len(moved.Nodes) != 1 || moved.Nodes[0] != "eu-1" {
		t.Errorf("an app that builds runs on %v, want the machine it was placed on", moved.Nodes)
	}
}

// With no domain there is no registry, and a build has nowhere to go: the
// image would be loaded into this machine's Docker and the machine that
// is to run it would have nowhere to pull it from. Refused in front of
// the person making the decision rather than minutes later, in a deploy
// on a box nobody is looking at.
func TestAnAppThatBuildsCannotLeaveAnInstanceWithNoRegistry(t *testing.T) {
	f := servertest.NewUnconfigured(t)
	_ = addServer(t, f, "eu-1")
	created := createBuildingApp(t, f, "worker")

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("moving an app that builds with no registry: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "domain") {
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
