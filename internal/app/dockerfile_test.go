package app_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

func railpackApp(name string) map[string]string {
	return map[string]string{
		"name": name, "domain": name + ".example.com", "org": "acme", "project": "web",
		"source": "railpack", "repo": "https://github.com/acme/api.git",
	}
}

func dockerfileApp(name string) map[string]string {
	return map[string]string{
		"name": name, "domain": name + ".example.com", "org": "acme", "project": "web",
		"source": "dockerfile", "repo": "https://github.com/acme/api.git",
	}
}

// Building runs whatever a repository contains, on this host, with the
// builder's privileges. Running an image someone already published does
// not, which is why one is an admin's and the other a member's.
func TestOnlyAnAdminMayCreateOrDeployABuildingApp(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	// A member may create an app that only runs published images.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "published", "domain": "published.example.com", "org": "acme", "project": "web",
		"source": "external", "image": "nginx",
	}, memberKey), http.StatusCreated)

	// But not one that builds, by either route.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps",
		dockerfileApp("built"), memberKey), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps",
		railpackApp("detected"), memberKey), http.StatusForbidden)

	// An admin creates it, and the member still cannot deploy it.
	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps",
		dockerfileApp("built"), f.AdminKey, &created), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", nil, memberKey), http.StatusForbidden)
}

// Someone outside the organization must not learn a building app exists
// by being told they lack the role for it.
func TestAnOutsiderStillGetsNotFoundOnABuildingApp(t *testing.T) {
	f := servertest.New(t)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps",
		dockerfileApp("built"), f.AdminKey, &created), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", nil, outsiderKey), http.StatusNotFound)
}

// A building app reports what it builds, not an image it would push.
func TestABuildingAppReportsItsRepository(t *testing.T) {
	f := servertest.New(t)

	body := dockerfileApp("built")
	body["ref"] = "main"
	body["dockerfile"] = "services/api/Dockerfile"

	var created struct {
		Reference  string `json:"reference"`
		Source     string `json:"source"`
		Repo       string `json:"repo"`
		Ref        string `json:"ref"`
		Dockerfile string `json:"dockerfile"`
		Image      string `json:"image"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", body, f.AdminKey, &created),
		http.StatusCreated)

	if created.Source != "dockerfile" || created.Repo != body["repo"] ||
		created.Ref != "main" || created.Dockerfile != "services/api/Dockerfile" {
		t.Errorf("created %+v", created)
	}
	// There is nothing to push to a building app, so no push path is
	// offered that would only mislead.
	if created.Image != "" {
		t.Errorf("a building app reported a push path: %q", created.Image)
	}
}

// A deploy of a building app on a daemon with no builder must refuse
// rather than crash. It cannot happen in a real install; a test server
// is exactly where it can.
func TestBuildingWithNoBuilderFailsTheDeployment(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps",
		dockerfileApp("built"), f.AdminKey, &created), http.StatusCreated)

	// Accepted — a build's outcome lives in its row, not in the response
	// to the request that asked for it.
	var deployment struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost,
		"/apps/"+created.Reference+"/deploy", nil, f.AdminKey, &deployment), http.StatusAccepted)

	var finished struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	rec := f.Do(t, http.MethodGet,
		"/apps/"+created.Reference+"/deployments/"+strconv.FormatInt(deployment.ID, 10)+"?wait=true", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &finished); err != nil {
		t.Fatal(err)
	}
	if finished.Status != "failed" {
		t.Fatalf("the deployment ended %q, want failed", finished.Status)
	}
	if !strings.Contains(finished.Error, "builder") {
		t.Errorf("the failure does not mention the builder: %q", finished.Error)
	}
}

// Railpack works the build out from the code, so there is no Dockerfile
// to name. A path it would ignore is a setting someone meant to have an
// effect, and accepting it silently is worse than refusing it.
func TestARailpackAppHasNoDockerfile(t *testing.T) {
	f := servertest.New(t)

	body := railpackApp("detected")
	body["dockerfile"] = "Dockerfile"
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", body, f.AdminKey),
		http.StatusBadRequest)

	var created struct {
		Source     string `json:"source"`
		Repo       string `json:"repo"`
		Dockerfile string `json:"dockerfile"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps",
		railpackApp("detected"), f.AdminKey, &created), http.StatusCreated)
	if created.Source != "railpack" || created.Repo == "" || created.Dockerfile != "" {
		t.Errorf("created %+v", created)
	}
}
