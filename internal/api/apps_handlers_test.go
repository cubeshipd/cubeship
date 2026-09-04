package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

// testProjectSlug is the project newTestServer creates in "acme" for
// every test, so a test that needs a project to create an app in can
// reference this literal slug instead of plumbing the *store.Project
// through every call site.
const testProjectSlug = "default"

// newTestServer returns a server backed by a fresh in-memory store, an
// organization "acme" (with a "default" project — see testProjectSlug —
// and its "production" environment), and an API key for a super-admin
// user — enough for tests that don't care about role boundaries. Tests
// that DO care about roles create their own additional users/memberships
// against srv.store directly.
func newTestServer(t *testing.T) (*Server, string, *store.Organization) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	org, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, _, err := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, testProjectSlug, "Default"); err != nil {
		t.Fatalf("CreateProjectWithDefaultEnvironment: %v", err)
	}
	user, err := s.CreateUser(ctx, "test-admin", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(ctx, user.ID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	return NewServer(s, nil, "webhook-secret", "registry.example.com"), key, org
}

func authedRequest(method, path string, body []byte, apiKey string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// testAPIKeyFor creates a user (super-admin if isSuperAdmin) and returns
// a fresh API key for them. Use this in tests that build their own
// store/server directly (e.g. with a custom orchestrator) instead of
// newTestServer.
func testAPIKeyFor(t *testing.T, s *store.Store, isSuperAdmin bool) string {
	t.Helper()
	username := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	user, err := s.CreateUser(context.Background(), username, isSuperAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(context.Background(), user.ID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return key
}

// testAPIKeyForExistingUser issues an API key for a user you already
// created, as opposed to testAPIKeyFor which creates the user too.
func testAPIKeyForExistingUser(t *testing.T, s *store.Store, userID int64) string {
	t.Helper()
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := s.CreateAPIKey(context.Background(), userID, authkey.Hash(key)); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return key
}

func TestCreateAppReturnsImagePath(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["image"] != "registry.example.com/acme/myapp" {
		t.Fatalf("expected image registry.example.com/acme/myapp, got %q", got["image"])
	}
}

func TestCreateAppMissingFields(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "org": org.Slug})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAppUnknownOrg(t *testing.T) {
	srv, key, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": "no-such-org", "project": testProjectSlug})
	req := authedRequest(http.MethodPost, "/apps", body, key)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCreateAppRequiresMembership(t *testing.T) {
	srv, _, org := newTestServer(t)
	ctx := context.Background()
	outsider, _ := srv.store.CreateUser(ctx, "outsider", false)
	outsiderKey, _ := authkey.Generate()
	srv.store.CreateAPIKey(ctx, outsider.ID, authkey.Hash(outsiderKey))

	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug})
	req := authedRequest(http.MethodPost, "/apps", body, outsiderKey)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCreateAppDuplicateName(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, "/apps", body, key))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, "/apps", body, key))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestListAndGetApp(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body, key))

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, authedRequest(http.MethodGet, "/apps", nil, key))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var apps []map[string]any
	json.Unmarshal(listRec.Body.Bytes(), &apps)
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	getRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(getRec, authedRequest(http.MethodGet, "/apps/myapp", nil, key))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	missRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(missRec, authedRequest(http.MethodGet, "/apps/nope", nil, key))
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missRec.Code)
	}
}

func TestGetAppHidesAppsFromOtherOrgs(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com", "org": org.Slug, "project": testProjectSlug})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body, key))

	ctx := context.Background()
	outsider, _ := srv.store.CreateUser(ctx, "outsider", false)
	outsiderKey, _ := authkey.Generate()
	srv.store.CreateAPIKey(ctx, outsider.ID, authkey.Hash(outsiderKey))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/apps/myapp", nil, outsiderKey))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (don't reveal the app exists to an outsider), got %d", rec.Code)
	}
}
