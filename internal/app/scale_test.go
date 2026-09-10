package app_test

import (
	"net/http"
	"testing"

	"cubeship/internal/server/servertest"
)

// **Asking for another copy takes effect now.** A worker creates the
// copy that is missing on its next pass, because reconciling is what
// its loop is for; the control plane had no such loop, so the same
// request wrote a row and left it without a container until somebody
// happened to redeploy.
func TestAnotherCopyStartsWithoutADeploy(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)
	after := len(docker.createdWith())

	place(t, f, created.Reference, map[string]any{"scale": 3})
	f.Server.Apps.WaitForDeploys()

	// Two more containers, and no new deployment: scaling is not a
	// deploy, and putting one in the history for a decision that
	// changed no code would be a rollout nobody made.
	if made := len(docker.createdWith()) - after; made != 2 {
		t.Errorf("%d copies were started for a scale of 1 to 3", made)
	}
	if got := replicaCount(t, f, created.Reference); got != 3 {
		t.Errorf("the app reports %d copies", got)
	}
	if n := deploymentCount(t, f, created.Reference); n != 1 {
		t.Errorf("scaling wrote %d deployments into the history", n)
	}
}

// And so does asking for fewer. A container left behind is one nothing
// on the instance names any more: invisible on every screen, holding
// its memory, and answering on the mesh under a name the proxy no
// longer sends anything to.
func TestACopyThatWasScaledAwayIsStoppedNow(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"scale": 3})
	deploy(t, f, created.Reference)

	place(t, f, created.Reference, map[string]any{"scale": 1})
	f.Server.Apps.WaitForDeploys()

	if len(docker.removedContainers()) == 0 {
		t.Error("nothing was stopped when the app went from three copies to one")
	}
	if got := replicaCount(t, f, created.Reference); got != 1 {
		t.Errorf("the app reports %d copies", got)
	}
}

func replicaCount(t *testing.T, f *servertest.Fixture, ref string) int {
	t.Helper()
	var out struct {
		Replicas []struct {
			Node string `json:"node"`
		} `json:"replicas"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref, nil, f.AdminKey, &out), http.StatusOK)
	return len(out.Replicas)
}

func deploymentCount(t *testing.T, f *servertest.Fixture, ref string) int {
	t.Helper()
	var out []struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+ref+"/deployments",
		nil, f.AdminKey, &out), http.StatusOK)
	return len(out)
}
