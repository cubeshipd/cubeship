package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAndListEnvironments(t *testing.T) {
	srv, key, org := newTestServer(t)
	base := "/orgs/" + org.Slug + "/projects/" + testProjectSlug

	body, _ := json.Marshal(map[string]string{"slug": "staging", "name": "Staging"})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, base+"/environments", body, key))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, authedRequest(http.MethodGet, base+"/environments", nil, key))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var envs []map[string]string
	json.Unmarshal(listRec.Body.Bytes(), &envs)
	// production (created with the project) + staging
	if len(envs) != 2 {
		t.Fatalf("expected 2 environments, got %d: %v", len(envs), envs)
	}
}

func TestCreateEnvironmentDuplicateSlugConflicts(t *testing.T) {
	srv, key, org := newTestServer(t)
	base := "/orgs/" + org.Slug + "/projects/" + testProjectSlug + "/environments"
	body, _ := json.Marshal(map[string]string{"slug": "staging", "name": "Staging"})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, base, body, key))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, base, body, key))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestSetEnvironmentEnv(t *testing.T) {
	srv, key, org := newTestServer(t)
	path := "/orgs/" + org.Slug + "/projects/" + testProjectSlug + "/environments/production/env"

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"LOG_LEVEL": "debug"}})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPut, path, body, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	project, err := srv.store.GetProjectBySlug(context.Background(), org.ID, testProjectSlug)
	if err != nil {
		t.Fatalf("GetProjectBySlug: %v", err)
	}
	env, err := srv.store.GetEnvironmentBySlug(context.Background(), project.ID, "production")
	if err != nil {
		t.Fatalf("GetEnvironmentBySlug: %v", err)
	}
	if env.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("unexpected environment env: %v", env.Env)
	}
}

func TestDeleteProductionEnvironmentIsRefused(t *testing.T) {
	srv, key, org := newTestServer(t)
	path := "/orgs/" + org.Slug + "/projects/" + testProjectSlug + "/environments/production"

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, path, nil, key))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	project, _ := srv.store.GetProjectBySlug(context.Background(), org.ID, testProjectSlug)
	if _, err := srv.store.GetEnvironmentBySlug(context.Background(), project.ID, "production"); err != nil {
		t.Fatalf("expected production environment to still exist: %v", err)
	}
}

func TestDeleteEnvironmentWithAppsIsRefused(t *testing.T) {
	srv, key, org := newTestServer(t)
	base := "/orgs/" + org.Slug + "/projects/" + testProjectSlug

	createEnv, _ := json.Marshal(map[string]string{"slug": "staging", "name": "Staging"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, base+"/environments", createEnv, key))

	createApp, _ := json.Marshal(map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug, "environment": "staging",
	})
	appRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(appRec, authedRequest(http.MethodPost, "/apps", createApp, key))
	if appRec.Code != http.StatusCreated {
		t.Fatalf("create app: expected 201, got %d: %s", appRec.Code, appRec.Body.String())
	}

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, base+"/environments/staging", nil, key))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteEmptyNonProductionEnvironmentSucceeds(t *testing.T) {
	srv, key, org := newTestServer(t)
	base := "/orgs/" + org.Slug + "/projects/" + testProjectSlug

	createEnv, _ := json.Marshal(map[string]string{"slug": "staging", "name": "Staging"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, base+"/environments", createEnv, key))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, base+"/environments/staging", nil, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAppCreateDefaultsToProductionEnvironment(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug,
	})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/apps", body, key))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["environment"] != "production" {
		t.Fatalf("expected environment to default to production, got %q", got["environment"])
	}
}
