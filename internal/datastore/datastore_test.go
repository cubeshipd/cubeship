package datastore_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// createDatastore posts one and returns the decoded response, failing
// the test if the daemon refused it.
func createDatastore(t *testing.T, f *servertest.Fixture, body map[string]any) map[string]any {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/datastores", body, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create datastore: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode datastore: %v", err)
	}
	return out
}

func createApp(t *testing.T, f *servertest.Fixture, name string) {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": f.Project.Slug, "environment": f.Environment.Slug,
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app %q: %d %s", name, rec.Code, rec.Body.String())
	}
}

// The whole point of the module: an app attached to a database receives
// the connection string, without anybody typing one.
//
// It is checked through the app's own env endpoint rather than through
// the datastore's, because that endpoint is what the deploy builds a
// container from — and it is where somebody goes to find out why an app
// is connecting where it is.
func TestAnAttachedAppInheritsTheConnectionString(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "api")
	createDatastore(t, f, map[string]any{
		"name": "pg", "project": f.Project.Slug, "engine": "postgres",
	})

	rec := f.Do(t, http.MethodPost, "/datastores/web/production/pg/attachments",
		map[string]any{"app": "api"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}

	effective := appEnv(t, f, "api")
	url, ok := effective["DATABASE_URL"]
	if !ok {
		t.Fatalf("the app inherited no DATABASE_URL: %v", effective)
	}
	if url.Source != envvar.SourceDatastore {
		t.Errorf("DATABASE_URL is labelled %q; nobody typed it, so it should say where it came from", url.Source)
	}
	if !strings.Contains(url.Value, "cubeship-db-web-production-pg") {
		t.Errorf("DATABASE_URL points at %q, not the datastore's container on the shared network", url.Value)
	}

	// An app's own variable still wins, which is how you point one
	// somewhere else without detaching anything.
	rec = f.Do(t, http.MethodPatch, "/apps/web/production/api/env",
		map[string]any{"set": map[string]string{"DATABASE_URL": "postgresql://elsewhere/db"}}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("set app env: %d %s", rec.Code, rec.Body.String())
	}
	effective = appEnv(t, f, "api")
	if got := effective["DATABASE_URL"]; got.Source != envvar.SourceApp {
		t.Errorf("the app's own DATABASE_URL lost to the datastore's: %+v", got)
	}

	// Detaching takes the rest of them away again.
	rec = f.Do(t, http.MethodDelete, "/datastores/web/production/pg/attachments/api", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", rec.Code, rec.Body.String())
	}
	if _, still := appEnv(t, f, "api")["DATABASE_HOST"]; still {
		t.Error("the app still inherits DATABASE_HOST after being detached")
	}
}

func appEnv(t *testing.T, f *servertest.Fixture, name string) map[string]envvar.Resolved {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/apps/web/production/"+name+"/env", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read app env: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Effective []envvar.Resolved `json:"effective"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode env: %v", err)
	}
	byKey := map[string]envvar.Resolved{}
	for _, r := range out.Effective {
		byKey[r.Key] = r
	}
	return byKey
}

// The password is stored as given, because a hash cannot connect to
// anything — so the only thing keeping it from leaking is that no
// listing carries it. Pinned here, the way the GitHub App's credentials
// are.
func TestThePasswordIsNeverInAListing(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	created := createDatastore(t, f, map[string]any{
		"name": "pg", "project": f.Project.Slug, "engine": "postgres",
		"password": "hunter2-and-then-some",
	})
	// Create is the one place it comes back, because a caller who left
	// the field empty has to be told what was generated.
	if created["password"] != "hunter2-and-then-some" {
		t.Errorf("create did not report the password: %v", created["password"])
	}

	for _, path := range []string{"/datastores", "/datastores/web/production/pg"} {
		rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("GET %s carries the password: %s", path, rec.Body.String())
		}
	}

	// It is readable, deliberately and on its own request.
	rec := f.Do(t, http.MethodGet, "/datastores/web/production/pg/credentials", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read credentials: %d %s", rec.Code, rec.Body.String())
	}
	var creds struct {
		Password    string `json:"password"`
		InternalURI string `json:"internal_uri"`
		ExternalURI string `json:"external_uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &creds); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if creds.Password != "hunter2-and-then-some" {
		t.Errorf("credentials reported %q", creds.Password)
	}
	if !strings.HasPrefix(creds.InternalURI, "postgresql://") {
		t.Errorf("internal URI is %q", creds.InternalURI)
	}
	if creds.ExternalURI != "" {
		t.Errorf("an unexposed datastore reported an external URI: %q", creds.ExternalURI)
	}
}

// Running a database costs disk on this host and its password reaches
// every app attached to it, so every write is an admin's. Reading which
// databases exist is not — you cannot understand what you are deploying
// into without it.
func TestOnlyAnAdminManagesDatabases(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "api")
	createDatastore(t, f, map[string]any{
		"name": "pg", "project": f.Project.Slug, "engine": "postgres",
	})
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	refused := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/datastores", map[string]any{"name": "other", "project": "web", "engine": "postgres"}},
		{http.MethodPatch, "/datastores/web/production/pg", map[string]any{"description": "mine now"}},
		{http.MethodDelete, "/datastores/web/production/pg", nil},
		{http.MethodGet, "/datastores/web/production/pg/credentials", nil},
		{http.MethodPost, "/datastores/web/production/pg/expose", map[string]any{}},
		{http.MethodDelete, "/datastores/web/production/pg/expose", nil},
		{http.MethodPost, "/datastores/web/production/pg/attachments", map[string]any{"app": "api"}},
		{http.MethodDelete, "/datastores/web/production/pg/attachments/api", nil},
	}
	for _, c := range refused {
		rec := f.Do(t, c.method, c.path, c.body, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d %s, want 403", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	allowed := []string{"/datastores", "/datastores/engines", "/datastores/web/production/pg"}
	for _, path := range allowed {
		rec := f.Do(t, http.MethodGet, path, nil, memberKey)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s as a member: %d %s, want 200", path, rec.Code, rec.Body.String())
		}
	}
}

// Deleting a project takes its databases the way it takes its apps. The
// rows would cascade away on their own — what has to happen here is the
// container being stopped and the data directory going with it, which
// only this module can do.
func TestDeletingAProjectTakesItsDatabases(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{
		"name": "pg", "project": f.Project.Slug, "engine": "postgres",
	})

	rec := f.Do(t, http.MethodDelete, "/projects/web", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete project: %d %s", rec.Code, rec.Body.String())
	}

	all, err := f.Server.Datastores.Repo().List(t.Context())
	if err != nil {
		t.Fatalf("list datastores: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("%d datastores survived the project they were in", len(all))
	}
}

// An app and a database in one environment are two different kinds of
// thing, named by two different people at two different times. Refusing
// the second one to be called "api" would be a rule with no reason
// behind it — the container names cannot collide.
func TestAnAppAndADatabaseMayShareAName(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "api")
	createDatastore(t, f, map[string]any{
		"name": "api", "project": f.Project.Slug, "engine": "postgres",
	})
}

// The version is what wrote the data directory, and no other version of
// the same engine can read it. A PATCH that quietly accepted one would
// be a container that will not start with the only copy of the data
// inside the directory it will not read.
func TestNeitherEngineNorVersionCanBeChanged(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	before := createDatastore(t, f, map[string]any{
		"name": "pg", "project": f.Project.Slug, "engine": "postgres", "version": "16",
	})

	rec := f.Do(t, http.MethodPatch, "/datastores/web/production/pg", map[string]any{
		"engine": "mysql", "version": "8.4", "name": "renamed", "username": "root",
	}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	var after map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"engine", "version", "name", "username"} {
		if after[field] != before[field] {
			t.Errorf("%s changed from %v to %v", field, before[field], after[field])
		}
	}
}

// A version this release does not offer is refused at creation, not
// discovered when the pull fails minutes later with nobody watching.
func TestAnUnknownEngineOrVersionIsRefused(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	for _, body := range []map[string]any{
		{"name": "a", "project": "web", "engine": "cockroach"},
		{"name": "b", "project": "web", "engine": "postgres", "version": "99"},
		{"name": "c", "project": "web", "engine": "mysql", "username": "root"},
	} {
		rec := f.Do(t, http.MethodPost, "/datastores", body, f.AdminKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %v: %d %s, want 400", body, rec.Code, rec.Body.String())
		}
	}
}

// Exposing is what puts a database on the internet, so the port it
// lands on has to be somebody's decision and no two may share one.
func TestExposingTakesAPortAndOnlyOneDatastoreGetsIt(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "project": "web", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "my", "project": "web", "engine": "mysql"})

	rec := f.Do(t, http.MethodPost, "/datastores/web/production/pg/expose", map[string]any{}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("expose: %d %s", rec.Code, rec.Body.String())
	}
	var exposed struct {
		Port         int    `json:"exposed_port"`
		ExternalHost string `json:"external_host"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exposed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exposed.Port < datastore.PortRangeStart || exposed.Port > datastore.PortRangeEnd {
		t.Errorf("automatic port %d is outside the range", exposed.Port)
	}
	if exposed.ExternalHost != servertest.Domain {
		t.Errorf("external host is %q, want the instance's domain", exposed.ExternalHost)
	}

	rec = f.Do(t, http.MethodPost, "/datastores/web/production/my/expose",
		map[string]any{"port": exposed.Port}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a second datastore took the same port: %d %s", rec.Code, rec.Body.String())
	}

	// And a port nothing may publish on.
	rec = f.Do(t, http.MethodPost, "/datastores/web/production/my/expose",
		map[string]any{"port": 80}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("port 80 was accepted: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodDelete, "/datastores/web/production/pg/expose", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpose: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "exposed_port") {
		t.Errorf("still reports a port after being unexposed: %s", rec.Body.String())
	}
}

// An app is attached by name within the datastore's own environment.
// One in another environment is not reachable by name here, and letting
// it through would wire staging to production data through a link
// neither screen shows.
func TestAttachingIsWithinOneEnvironment(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "project": "web", "engine": "postgres"})

	rec := f.Do(t, http.MethodPost, "/projects/web/environments",
		map[string]any{"slug": "staging"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create environment: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": "api", "project": "web", "environment": "staging",
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create staging app: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores/web/production/pg/attachments",
		map[string]any{"app": "api"}, f.AdminKey)
	if rec.Code != http.StatusNotFound {
		t.Errorf("attached an app from another environment: %d %s", rec.Code, rec.Body.String())
	}
}

// One app, two databases, and the prefix is what keeps them from being
// one variable with two values.
func TestASecondDatabaseOnOneAppNeedsAPrefix(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "api")
	createDatastore(t, f, map[string]any{"name": "main", "project": "web", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "analytics", "project": "web", "engine": "postgres"})

	rec := f.Do(t, http.MethodPost, "/datastores/web/production/main/attachments",
		map[string]any{"app": "api"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attach the first: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores/web/production/analytics/attachments",
		map[string]any{"app": "api"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a second database landed on the same variables: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores/web/production/analytics/attachments",
		map[string]any{"app": "api", "prefix": "ANALYTICS_"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("attach with a prefix: %d %s", rec.Code, rec.Body.String())
	}

	env := appEnv(t, f, "api")
	for _, key := range []string{"DATABASE_URL", "ANALYTICS_DATABASE_URL"} {
		if _, ok := env[key]; !ok {
			t.Errorf("the app did not inherit %s", key)
		}
	}
	if env["DATABASE_URL"].Value == env["ANALYTICS_DATABASE_URL"].Value {
		t.Error("both databases resolved to the same connection string")
	}
}
