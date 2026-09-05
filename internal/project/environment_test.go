package project_test

import (
	"net/http"
	"testing"

	"cubeship/internal/server/servertest"
)

// Every project must always have somewhere to put an app, which is what
// makes production undeletable.
func TestProductionEnvironmentCannotBeDeleted(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodDelete, "/orgs/acme/projects/web/environments/production", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusForbidden)

	var envs []struct{ Slug string }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/orgs/acme/projects/web/environments", nil, f.AdminKey, &envs), http.StatusOK)
	if len(envs) != 1 || envs[0].Slug != "production" {
		t.Fatalf("expected production to survive, got %v", envs)
	}
}

// Deleting an environment takes the apps deployed in it. Refusing
// instead would leave someone deleting apps one at a time to reach the
// thing they actually asked to delete.
func TestDeletingAnEnvironmentTakesItsAppsWithIt(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/projects/web/environments",
		map[string]string{"slug": "staging"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": "staging-app", "org": "acme", "project": "web", "environment": "staging",
	}, f.AdminKey), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web/environments/staging", nil, f.AdminKey), http.StatusOK)

	if rec := f.Do(t, http.MethodGet, "/apps/acme/web/staging/staging-app", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("the app survived its environment: %d", rec.Code)
	}
}

func TestEmptyNonProductionEnvironmentCanBeDeleted(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/orgs/acme/projects/web/environments",
		map[string]string{"slug": "preview", "name": "Preview"}, f.AdminKey), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete,
		"/orgs/acme/projects/web/environments/preview", nil, f.AdminKey), http.StatusOK)

	var envs []struct{ Slug string }
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/orgs/acme/projects/web/environments", nil, f.AdminKey, &envs), http.StatusOK)
	if len(envs) != 1 {
		t.Fatalf("expected only production to remain, got %v", envs)
	}
}

// A new project always arrives with production, so an app can be created
// in it immediately.
func TestNewProjectComesWithProduction(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Environments []string `json:"environments"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/orgs/acme/projects",
		map[string]string{"slug": "api", "name": "API"}, f.AdminKey, &created), http.StatusCreated)

	if len(created.Environments) != 1 || created.Environments[0] != "production" {
		t.Fatalf("expected a production environment, got %v", created.Environments)
	}
}

// Slugs become path components of registry image references, so anything
// Docker would reject in a repository path has to be refused here.
func TestSlugsAreRejectedWhenTheyWouldBreakARegistryPath(t *testing.T) {
	f := servertest.New(t)

	bad := []string{"MyApp", "my app", "café", "my_app", "my/app", "-myapp", "myapp-", ".", ""}

	for _, name := range bad {
		t.Run("app "+name, func(t *testing.T) {
			rec := f.Do(t, http.MethodPost, "/apps", map[string]string{
				"name": name, "org": "acme", "project": "web",
			}, f.AdminKey)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("app name %q was accepted (%d): %s", name, rec.Code, rec.Body.String())
			}
		})
		t.Run("project "+name, func(t *testing.T) {
			rec := f.Do(t, http.MethodPost, "/orgs/acme/projects",
				map[string]string{"slug": name, "name": "X"}, f.AdminKey)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("project slug %q was accepted (%d): %s", name, rec.Code, rec.Body.String())
			}
		})
	}
}
