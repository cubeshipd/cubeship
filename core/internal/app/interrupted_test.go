package app_test

import (
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/server/servertest"
)

// A deploy the daemon was running when it was replaced — by an update,
// most of all — has nobody left to finish it. Before SettleInterrupted
// it said `pending` for ever and could not be deleted.
func TestADeployARestartInterruptedIsClosedAtStartup(t *testing.T) {
	f := balancerFixture(t)
	_ = addServer(t, f, "eu-1")

	here := createExternalApp(t, f, "api")
	elsewhere := createExternalApp(t, f, "worker")
	place(t, f, elsewhere.Reference, map[string]any{"nodes": []string{"eu-1"}})

	// What a restart leaves behind, written as the orchestrator leaves it.
	building := pendingDeploy(t, f, "api", "master")
	swapping := pendingDeploy(t, f, "api", "docker.io/library/nginx:1.27")
	waiting := pendingDeploy(t, f, "worker", "docker.io/library/nginx:1.27")

	if err := app.SettleInterrupted(t.Context(), f.Server.Apps.Repo()); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what string
		ref  string
		id   int64
		want string
	}{
		{"a build that never produced an image", here.Reference, building, app.DeploymentFailed},
		{"a swap this machine never did", here.Reference, swapping, app.DeploymentFailed},
		{"a rollout waiting on another machine", elsewhere.Reference, waiting, app.DeploymentPending},
	} {
		if got := deploymentStatus(t, f, tc.ref, tc.id); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.what, got, tc.want)
		}
	}
}

// pendingDeploy writes an unfinished deploy row for the app called name,
// the way StartDeployment and SetDeploymentImage leave one.
func pendingDeploy(t *testing.T, f *servertest.Fixture, name, imageRef string) int64 {
	t.Helper()
	var appID, id int64
	if err := f.DB.QueryRowContext(t.Context(), `SELECT id FROM apps WHERE name = $1`, name).Scan(&appID); err != nil {
		t.Fatalf("find app %s: %v", name, err)
	}
	if err := f.DB.QueryRowContext(t.Context(),
		`INSERT INTO deployments (app_id, image_ref, status) VALUES ($1, $2, $3) RETURNING id`,
		appID, imageRef, app.DeploymentPending).Scan(&id); err != nil {
		t.Fatalf("write a pending deploy for %s: %v", name, err)
	}
	return id
}
