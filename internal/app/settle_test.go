package app_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/node"
	"cubeship/internal/server/servertest"
)

// A deploy has to end.
//
// This one has nothing left to wait for: the machine it was waiting on
// was taken off the app before it ever ran it, and the machine that did
// run it is running exactly this deployment. If the row is still
// `pending` here, nothing will ever move it — settle is only reached
// from a machine's report, and no report is coming.
//
// It is worse than a wrong label. A deployment that has not finished is
// refused for deletion, so the row cannot be cleared either, and the
// screen watching it polls a deploy that is over.
func TestADeployEndsWhenTheMachineItWasWaitingOnLeavesTheApp(t *testing.T) {
	f := balancerFixture(t)
	_ = addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})

	d := deploy(t, f, created.Reference)
	if got := deploymentStatus(t, f, created.Reference, d); got != app.DeploymentPending {
		t.Fatalf("with eu-1 still to report, the deploy is %q", got)
	}

	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane"}})

	if got := deploymentStatus(t, f, created.Reference, d); got != app.DeploymentSucceeded {
		t.Errorf("the deploy is %q with every machine that runs the app running it", got)
	}
}

// The same shape from the other side: a machine reports a deployment
// for an app it has since been taken off. Its report is dropped — the
// machines the app is on now are the ones whose reports count — and
// that must not leave the row open when those machines are all in.
func TestAReportFromAMachineThatLeftDoesNotHoldADeployOpen(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})

	d := deploy(t, f, created.Reference)
	answer := reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("eu-1 was told to run %d apps", len(answer.Desired.Apps))
	}
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane"}})
	reconcile(t, f, token, node.Result{
		App: created.Reference, Deploy: answer.Desired.Apps[0].Deploy, Container: "container-on-eu-1",
	})

	if got := deploymentStatus(t, f, created.Reference, d); got != app.DeploymentSucceeded {
		t.Errorf("the deploy is %q after the only machine still running it reported", got)
	}
}

// A deploy waiting on a machine that has stopped answering is not
// something anybody can clear today: a deploy that has not finished is
// refused for deletion, so the record is permanent and the screen
// watching it polls a rollout nobody is working on.
//
// It stays `pending` on purpose — a machine that comes back picks it up
// and finishes what it missed — so what has to change is what is said
// about it, and whether the record can be cleared by somebody who
// decides the rollout is not happening.
func TestADeployWaitingOnAMachineThatIsGoneSaysSoAndCanBeCleared(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"nodes": []string{"control-plane", "eu-1"}})

	// The machine calls in once, so it is a machine this instance has
	// heard from, and then never again.
	reconcile(t, f, token)
	d := deploy(t, f, created.Reference)

	// While it is still answering, nothing is stalled: a deploy in its
	// first minutes is a deploy in progress, and a machine that is
	// merely slow is still calling in.
	if got := deploymentOf(t, f, created.Reference, d); len(got.StalledOn) != 0 || got.Deletable {
		t.Fatalf("a fresh deploy is already stalled: %+v", got)
	}

	// Now it has been gone long enough to stop being a blip.
	silence(t, f, "eu-1", app.StuckAfter+time.Minute)

	got := deploymentOf(t, f, created.Reference, d)
	if len(got.StalledOn) != 1 || got.StalledOn[0] != "eu-1" {
		t.Errorf("the deploy does not say who it is waiting for: %+v", got)
	}
	if got.Status != app.DeploymentPending {
		t.Errorf("the deploy is %q; a machine that comes back has to be able to finish it", got.Status)
	}
	if !got.Deletable {
		t.Fatalf("the record cannot be cleared, so it is permanent: %+v", got)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		fmt.Sprintf("/apps/%s/deployments/%d", created.Reference, d), nil, f.AdminKey),
		http.StatusNoContent)
}

// silence backdates when a machine last called in, which is the only
// thing that decides whether it is answering. Reaching into the table
// rather than waiting: the alternative is a test that sleeps for
// fifteen minutes.
func silence(t *testing.T, f *servertest.Fixture, name string, ago time.Duration) {
	t.Helper()
	if _, err := f.DB.ExecContext(t.Context(),
		`UPDATE nodes SET last_seen_at = now() - $2::interval WHERE slug = $1`,
		name, fmt.Sprintf("%d seconds", int(ago.Seconds()))); err != nil {
		t.Fatalf("age out %s: %v", name, err)
	}
}

type deploymentView struct {
	Status    string   `json:"status"`
	Deletable bool     `json:"deletable"`
	StalledOn []string `json:"stalled_on"`
}

func deploymentOf(t *testing.T, f *servertest.Fixture, ref string, id int64) deploymentView {
	t.Helper()
	var out deploymentView
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		fmt.Sprintf("/apps/%s/deployments/%d", ref, id), nil, f.AdminKey, &out), http.StatusOK)
	return out
}
