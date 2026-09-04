package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

// twoOrgFixture is the multi-tenant shape the authorization rules exist
// for: two organizations, each with an app, and a plain member in the
// first who has no relationship at all to the second.
type twoOrgFixture struct {
	store     *store.Store
	acme      *store.Organization
	globex    *store.Organization
	memberKey string
}

func newTwoOrgFixture(t *testing.T, docker *webhookFakeDocker) (*Server, twoOrgFixture) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	acme, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	globex, err := s.CreateOrganization(ctx, "globex", "Globex Corp")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	acmeProject, acmeEnv, err := s.CreateProjectWithDefaultEnvironment(ctx, acme.ID, "default", "Default")
	if err != nil {
		t.Fatalf("CreateProjectWithDefaultEnvironment: %v", err)
	}
	globexProject, globexEnv, err := s.CreateProjectWithDefaultEnvironment(ctx, globex.ID, "default", "Default")
	if err != nil {
		t.Fatalf("CreateProjectWithDefaultEnvironment: %v", err)
	}
	if _, err := s.CreateApp(ctx, acme.ID, acmeProject.ID, acmeEnv.ID, "acmeapp", "acmeapp.example.com", "registry.example.com/acme/acmeapp"); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if _, err := s.CreateApp(ctx, globex.ID, globexProject.ID, globexEnv.ID, "globexapp", "globexapp.example.com", "registry.example.com/globex/globexapp"); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}

	member, err := s.CreateUser(ctx, "acme-member", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.AddMembership(ctx, member.ID, acme.ID, store.RoleMember); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "webhook-secret", "registry.example.com")
	return srv, twoOrgFixture{
		store:     s,
		acme:      acme,
		globex:    globex,
		memberKey: testAPIKeyForExistingUser(t, s, member.ID),
	}
}

// The spec's own stated case: an org admin can add a member.
func TestCreateOrgUserAsOrgAdminNotSuperAdmin(t *testing.T) {
	srv, fx := newTwoOrgFixture(t, &webhookFakeDocker{})
	ctx := context.Background()
	admin, _ := fx.store.CreateUser(ctx, "acme-admin", false)
	fx.store.AddMembership(ctx, admin.ID, fx.acme.ID, store.RoleAdmin)
	adminKey := testAPIKeyForExistingUser(t, fx.store, admin.ID)

	rec := createOrgUser(t, srv, fx.acme.Slug, "employee1", "member", adminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected an org admin to create a user, got %d: %s", rec.Code, rec.Body.String())
	}

	// ...but only in their own org.
	other := createOrgUser(t, srv, fx.globex.Slug, "employee2", "member", adminKey)
	if other.Code != http.StatusForbidden {
		t.Fatalf("expected 403 in an org they don't administer, got %d", other.Code)
	}
}

// ...and the other half of it: a member cannot.
func TestCreateOrgUserAsOrgMemberIsDenied(t *testing.T) {
	srv, fx := newTwoOrgFixture(t, &webhookFakeDocker{})

	rec := createOrgUser(t, srv, fx.acme.Slug, "employee1", "member", fx.memberKey)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a member of the org, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := fx.store.GetUserByUsername(context.Background(), "employee1"); err == nil {
		t.Fatal("a denied request must not create the user")
	}
}

// The multi-tenant happy path: a plain member, not a super-admin, works
// on their own org's app.
func TestMemberCanUseOwnOrgApp(t *testing.T) {
	docker := &webhookFakeDocker{running: true}
	srv, fx := newTwoOrgFixture(t, docker)

	deployRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(deployRec, authedRequest(http.MethodPost, "/apps/acmeapp/deploy", []byte(`{"tag":"v2"}`), fx.memberKey))
	if deployRec.Code != http.StatusOK {
		t.Fatalf("expected a member to deploy their org's app, got %d: %s", deployRec.Code, deployRec.Body.String())
	}
	if docker.pulledRef != "127.0.0.1:5000/acme/acmeapp:v2" {
		t.Fatalf("expected the deploy to pull the app's image, got %q", docker.pulledRef)
	}

	envBody, _ := json.Marshal(map[string]map[string]string{"vars": {"PORT": "9090"}})
	envRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(envRec, authedRequest(http.MethodPut, "/apps/acmeapp/env", envBody, fx.memberKey))
	if envRec.Code != http.StatusOK {
		t.Fatalf("expected a member to set env on their org's app, got %d", envRec.Code)
	}

	getRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(getRec, authedRequest(http.MethodGet, "/apps/acmeapp", nil, fx.memberKey))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected a member to read their org's app, got %d", getRec.Code)
	}
}

// Env vars carry secrets: another org's member must not reach them, in
// either direction.
func TestSetEnvDeniedAcrossOrgs(t *testing.T) {
	srv, fx := newTwoOrgFixture(t, &webhookFakeDocker{})

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"STOLEN": "1"}})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPut, "/apps/globexapp/env", body, fx.memberKey))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another org's app, got %d: %s", rec.Code, rec.Body.String())
	}

	app, err := fx.store.GetAppByName(context.Background(), "globexapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if _, ok := app.Env["STOLEN"]; ok {
		t.Fatalf("a denied request must not write env, got %v", app.Env)
	}
}

func TestGetLogsDeniedAcrossOrgs(t *testing.T) {
	docker := &webhookFakeDocker{running: true, logsContent: dockerStdoutFrame("secret log line\n")}
	srv, fx := newTwoOrgFixture(t, docker)
	ctx := context.Background()
	app, _ := fx.store.GetAppByName(ctx, "globexapp")
	fx.store.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/apps/globexapp/logs", nil, fx.memberKey))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another org's logs, got %d", rec.Code)
	}
	if rec.Body.String() == "secret log line\n" {
		t.Fatal("another org's log output must not be served")
	}
}

// Listing must filter, not just count: acme's member sees acme's app and
// nothing of globex's.
func TestListAppsExcludesOtherOrgsApps(t *testing.T) {
	srv, fx := newTwoOrgFixture(t, &webhookFakeDocker{})

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/apps", nil, fx.memberKey))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var apps []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &apps); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(apps) != 1 || apps[0]["name"] != "acmeapp" {
		t.Fatalf("expected only the caller's own org's app, got %v", apps)
	}
}
