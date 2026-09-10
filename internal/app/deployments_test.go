package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
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

// Deleting the deploy an app is running takes the app down — and leaves
// everything else about the app standing.
//
// That is the case this exists for: an app whose running version has to
// go now should not force somebody to delete the app and lose its
// domains, its environment and everything attached to it.
func TestDeletingTheLiveDeployTakesTheAppDownAndKeepsTheApp(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.NewWithDocker(t, quietDocker{})

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

	old, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:1")
	if err := repo.FinishDeployment(ctx, old.ID, app.DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	live, _ := repo.StartDeployment(ctx, scoped.ID, "nginx:2")
	if err := repo.FinishDeployment(ctx, live.ID, app.DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	// A container, which is what makes one of those rows the live one.
	here, err := repo.ControlPlaneID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateContainer(ctx, scoped.ID, here, 1, "container-abc", "container-abc", live.ID, app.StatusRunning); err != nil {
		t.Fatal(err)
	}

	if got := deploymentsOf(t, f); !got[live.ID].Live || got[old.ID].Live {
		t.Errorf("the newest success is not the one reported as live: %+v", got)
	}

	base := "/apps/web/production/api/deployments/"
	rec = f.Do(t, http.MethodDelete, base+strconv.FormatInt(live.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete the live deploy: %d %s", rec.Code, rec.Body.String())
	}

	// The app is still there, with everything about it.
	var after struct {
		Status       string `json:"status"`
		HasContainer bool   `json:"has_container"`
		Image        string `json:"image"`
	}
	rec = f.Do(t, http.MethodGet, "/apps/web/production/api", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("the app went with its deployment: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after.HasContainer {
		t.Error("the app still has a container after its live deploy was deleted")
	}
	if after.Status != "down" {
		t.Errorf("status = %q, want down", after.Status)
	}
	if after.Image == "" {
		t.Error("the app lost what it pulls, which is configuration rather than a deploy")
	}

	// And nothing inherits the title: an app with no container is
	// running no deployment, so what is left is all history.
	remaining := deploymentsOf(t, f)
	if remaining[old.ID].Live {
		t.Error("the deploy under the one that was deleted became live without anything starting")
	}
	if !remaining[old.ID].Deletable {
		t.Error("history on a stopped app is not deletable")
	}
}

// A deploy that has not finished is the one refusal left: the daemon is
// still writing to that row.
func TestADeployStillRunningCannotBeDeleted(t *testing.T) {
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
		t.Fatal(err)
	}
	running, err := app.NewRepository(f.DB).StartDeployment(ctx, scoped.ID, "nginx:1")
	if err != nil {
		t.Fatal(err)
	}

	if deploymentsOf(t, f)[running.ID].Deletable {
		t.Error("a deploy still running reports itself as deletable")
	}
	rec = f.Do(t, http.MethodDelete,
		"/apps/web/production/api/deployments/"+strconv.FormatInt(running.ID, 10), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("deleting a running deploy: %d %s, want 409", rec.Code, rec.Body.String())
	}
}

type deploymentFlags struct {
	Deletable bool `json:"deletable"`
	Live      bool `json:"live"`
}

func deploymentsOf(t *testing.T, f *servertest.Fixture) map[int64]deploymentFlags {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/apps/web/production/api/deployments", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list deployments: %d %s", rec.Code, rec.Body.String())
	}
	var list []struct {
		ID int64 `json:"id"`
		deploymentFlags
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	out := map[int64]deploymentFlags{}
	for _, d := range list {
		out[d.ID] = d.deploymentFlags
	}
	return out
}

// quietDocker is an Engine that says yes. These tests are about which
// records may go and what that does to the app's row, not about what
// Docker did — and servertest's default answers "no Docker here", which
// would make every one of them a failure to stop a container.
type quietDocker struct{}

func (quietDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }
func (quietDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "container-abc", nil
}
func (quietDocker) StartContainer(context.Context, string) error  { return nil }
func (quietDocker) StopContainer(context.Context, string) error   { return nil }
func (quietDocker) RemoveContainer(context.Context, string) error { return nil }
func (quietDocker) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (quietDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
