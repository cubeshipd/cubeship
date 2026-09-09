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
}

// addServer puts a machine in the cluster. Nothing is contacted — the
// row is a place for a box that does not exist — which is exactly what
// these tests want: the placement decisions are the control plane's,
// and none of them wait for a machine to answer.
func addServer(t *testing.T, f *servertest.Fixture, name string) {
	t.Helper()
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/nodes",
		map[string]any{"name": name}, f.AdminKey), http.StatusCreated)
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

	addServer(t, f, "eu-1")
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

// An app with a name to answer at cannot leave the control plane. Each
// machine is its own edge and only this one routes traffic, so a moved
// app would deploy, run, and answer nothing at the address it is
// supposed to — which is the kind of broken nobody notices until
// somebody complains.
func TestAnAppWithADomainStaysWhereTheTrafficArrives(t *testing.T) {
	f := servertest.New(t)
	addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "web")

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "web.example.com"}, f.AdminKey), http.StatusCreated)

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"node": "eu-1"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("moving an app with a domain: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "web.example.com") {
		t.Errorf("the refusal is %q, and it has to name what is in the way", rec.Body.String())
	}
}

// An app built here cannot run there. The image is loaded into this
// machine's Docker rather than pushed anywhere, so another machine has
// nowhere to pull it from — and a placement that cannot be pulled is a
// deploy that fails on a box nobody is looking at.
func TestAnAppBuiltHereCannotRunElsewhereYet(t *testing.T) {
	f := servertest.New(t)
	addServer(t, f, "eu-1")

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
	addServer(t, f, "eu-1")
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
