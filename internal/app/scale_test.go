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

// **A switch, not a third way of naming machines.** An app that follows
// the cluster gets a share of every machine there is, including the ones
// that join afterwards — which is the thing `nodes` and `scale` cannot
// say however carefully they are set.
func TestAnAppCanFollowTheCluster(t *testing.T) {
	f := balancerFixture(t)
	created := createExternalApp(t, f, "api")

	on := place(t, f, created.Reference, map[string]any{"spread": true})
	if len(on.Nodes) != 1 {
		t.Fatalf("with one machine in the cluster it runs on %v", on.Nodes)
	}

	// A machine joins. Nothing is said about the app.
	_ = addServer(t, f, "eu-1")

	var after placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference,
		nil, f.AdminKey, &after), http.StatusOK)
	if len(after.Nodes) != 2 {
		t.Errorf("after a server joined, the app runs on %v", after.Nodes)
	}
	if !after.Spread {
		t.Error("the app stopped following the cluster on its own")
	}
}

// Naming machines is choosing by hand, which is the opposite of
// following the cluster. Turned off rather than refused: what somebody
// just said is what they want.
func TestNamingMachinesTurnsTheSwitchOff(t *testing.T) {
	f := balancerFixture(t)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"spread": true})
	_ = addServer(t, f, "eu-1")

	back := place(t, f, created.Reference, map[string]any{"nodes": []string{"eu-1"}})
	if back.Spread {
		t.Error("it still follows the cluster after being placed by hand")
	}
	if len(back.Nodes) != 1 || back.Nodes[0] != "eu-1" {
		t.Errorf("it runs on %v", back.Nodes)
	}
}

// A machine carrying an app that follows the cluster can still be
// removed: the app leaves it as it goes. One carrying an app somebody
// placed by hand cannot, because where that one should go is a decision
// and making it by deleting a row would make it invisibly.
func TestAMachineRunningAFollowingAppCanStillBeRemoved(t *testing.T) {
	f := balancerFixture(t)
	created := createExternalApp(t, f, "api")
	_ = addServer(t, f, "eu-1")
	on := place(t, f, created.Reference, map[string]any{"spread": true})
	if len(on.Nodes) != 2 {
		t.Fatalf("the app runs on %v", on.Nodes)
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/nodes/eu-1", nil, f.AdminKey), http.StatusOK)

	var after placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference,
		nil, f.AdminKey, &after), http.StatusOK)
	if len(after.Nodes) != 1 {
		t.Errorf("after the server went, the app runs on %v", after.Nodes)
	}
}

// A machine joining takes copies **away** from the ones already there:
// two on one box becomes one each on two. The containers those copies
// were have to be stopped, or a re-spread leaks one per machine per
// join — running, invisible on every screen, and answering on the mesh
// under a name nothing points at.
func TestARespreadStopsTheCopiesItMovedAway(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"spread": true, "scale": 2})
	deploy(t, f, created.Reference)
	f.Server.Apps.WaitForDeploys()
	before := len(docker.removedContainers())

	// A machine joins, so the two copies become one each.
	_ = addServer(t, f, "eu-1")
	f.Server.Apps.WaitForDeploys()

	if len(docker.removedContainers()) == before {
		t.Error("the copy this machine gave up is still running")
	}
	var after placedApp
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/apps/"+created.Reference,
		nil, f.AdminKey, &after), http.StatusOK)
	if len(after.Nodes) != 2 {
		t.Errorf("the app runs on %v", after.Nodes)
	}
}
