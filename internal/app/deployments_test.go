package app_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

type deploymentRow struct {
	ID      int64  `json:"id"`
	Status  string `json:"status"`
	Logs    string `json:"logs"`
	HasLogs bool   `json:"has_logs"`
}

// A build's output is capped at 256 KiB and a history is fifty rows, so
// a listing that carried the logs would be twelve megabytes — fetched
// every couple of seconds by anything watching a build, which is
// exactly when somebody is watching. The listing says whether there is
// output; reading one deployment is what hands it over.
func TestADeploymentListingCarriesNoLogs(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": "api", "project": "web",
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", rec.Code, rec.Body.String())
	}

	// A deployment with something in its log, written the way a build
	// writes one.
	ctx := t.Context()
	scoped, err := f.Server.Apps.Resolve(ctx, f.Admin,
		app.Reference{Project: "web", Environment: "production", Name: "api"}, user.RoleAdmin)
	if err != nil {
		t.Fatalf("find the app: %v", err)
	}
	repo := app.NewRepository(f.DB)
	deployment, err := repo.StartDeployment(ctx, scoped.ID, "nginx:latest")
	if err != nil {
		t.Fatalf("start deployment: %v", err)
	}
	output := strings.Repeat("step 1/9 : FROM golang\n", 200)
	if err := repo.SetDeploymentLogs(ctx, deployment.ID, output); err != nil {
		t.Fatalf("write the log: %v", err)
	}
	if err := repo.FinishDeployment(ctx, deployment.ID, app.DeploymentSucceeded, ""); err != nil {
		t.Fatalf("finish deployment: %v", err)
	}

	var list []deploymentRow
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api/deployments", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list deployments: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no deployments came back")
	}
	for _, d := range list {
		if d.Logs != "" {
			t.Errorf("the listing carried %d bytes of log for deployment %d", len(d.Logs), d.ID)
		}
	}
	if !list[0].HasLogs {
		t.Error("the listing does not say there is a log to read")
	}

	// And reading the one deployment hands it over.
	var one deploymentRow
	rec = f.Do(t, http.MethodGet,
		"/apps/web/production/api/deployments/"+strconv.FormatInt(list[0].ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read deployment: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Logs != output {
		t.Errorf("reading one deployment gave %d bytes of log, want %d", len(one.Logs), len(output))
	}
	if !one.HasLogs {
		t.Error("a deployment holding a log does not say so")
	}
}

// Two records may not go, and the reason is different for each.
//
// A deploy still running is one the orchestrator is writing to. The
// deploy the app is *running* is the only thing that says what the
// container is — deleting it would leave something running that nothing
// on the instance explains. Everything else, and a failed deploy above
// all, is history somebody should be able to clear.
func TestOnlyAFinishedDeployThatIsNotLiveCanBeDeleted(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": "api", "project": "web",
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app: %d %s", rec.Code, rec.Body.String())
	}

	ctx := t.Context()
	scoped, err := f.Server.Apps.Resolve(ctx, f.Admin,
		app.Reference{Project: "web", Environment: "production", Name: "api"}, user.RoleAdmin)
	if err != nil {
		t.Fatalf("find the app: %v", err)
	}
	repo := app.NewRepository(f.DB)

	// Three deploys: one that failed, one that succeeded and was
	// replaced, and the newest success — which is what the app runs.
	failed, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:1")
	if err := repo.FinishDeployment(ctx, failed.ID, app.DeploymentFailed, "it did not build"); err != nil {
		t.Fatal(err)
	}
	old, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:2")
	if err := repo.FinishDeployment(ctx, old.ID, app.DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	live, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:3")
	if err := repo.FinishDeployment(ctx, live.ID, app.DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	running, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:4")

	base := "/apps/web/production/api/deployments/"

	// The listing says which is which before anybody tries.
	var list []struct {
		ID        int64 `json:"id"`
		Deletable bool  `json:"deletable"`
	}
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api/deployments", nil, f.AdminKey)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	deletable := map[int64]bool{}
	for _, d := range list {
		deletable[d.ID] = d.Deletable
	}
	for id, want := range map[int64]bool{
		failed.ID: true, old.ID: true, live.ID: false, running.ID: false,
	} {
		if deletable[id] != want {
			t.Errorf("deployment %d: deletable = %v, want %v", id, deletable[id], want)
		}
	}

	// The one still running is refused: the deploy is writing to it.
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(running.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("deleting a running deploy: %d %s, want 409", rec.Code, rec.Body.String())
	}

	// So is the one the app is running.
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(live.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("deleting the live deploy: %d %s, want 409", rec.Code, rec.Body.String())
	}

	// The failed one goes, which is the whole point.
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(failed.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deleting a failed deploy: %d %s", rec.Code, rec.Body.String())
	}
	// And so does an older success, which is history too.
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(old.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deleting a superseded deploy: %d %s", rec.Code, rec.Body.String())
	}

	// **The app is untouched.** This deletes records, not containers.
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("the app went with its history: %d %s", rec.Code, rec.Body.String())
	}

	// Deleting an older success promotes nothing: the live one is still
	// the live one, and still refused.
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(live.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("the live deploy became deletable once its predecessor went: %d", rec.Code)
	}
}
