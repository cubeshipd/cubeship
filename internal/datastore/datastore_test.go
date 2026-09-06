package datastore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/project"
	"cubeship/internal/server/servertest"
	"cubeship/internal/slug"
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

// createApp makes an app in an existing project and environment.
func createApp(t *testing.T, f *servertest.Fixture, projectSlug, env, name string) {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": projectSlug, "environment": env,
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app %q: %d %s", name, rec.Code, rec.Body.String())
	}
}

func attach(t *testing.T, f *servertest.Fixture, name, appRef, prefix string) *httptest.ResponseRecorder {
	t.Helper()
	return f.Do(t, http.MethodPost, "/datastores/"+name+"/attachments",
		map[string]any{"app": appRef, "prefix": prefix}, f.AdminKey)
}

// Every error this module re-raises has to arrive as the status it
// means. It resolves apps — attaching names one — so app's errors reach
// its handlers, and a chain that skipped app's mapping turned "no such
// app" into a 500 carrying the right sentence: the worst of both, since
// a caller retries a 500 and reads a 404.
//
// No database: this is the mapping itself, and it is the part that was
// wrong.
func TestErrorsFromEveryModuleBelowArriveAsTheStatusTheyMean(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		// This module's own.
		{datastore.ErrNotFound, http.StatusNotFound},
		{datastore.ErrAlreadyExists, http.StatusConflict},
		{datastore.ErrPortTaken, http.StatusConflict},
		{datastore.ErrBadPort, http.StatusBadRequest},
		{datastore.ErrUnknownEngine, http.StatusBadRequest},
		{datastore.ErrReservedSlug, http.StatusBadRequest},
		// app's, reached by resolving the app an attachment names.
		{app.ErrNotFound, http.StatusNotFound},
		// project's, and user's below it.
		{project.ErrNotFound, http.StatusNotFound},
		{user.ErrForbidden, http.StatusForbidden},
		{user.ErrUnauthenticated, http.StatusUnauthorized},
		{slug.ErrInvalid, http.StatusBadRequest},
		{slug.ErrReserved, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		datastore.WriteError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("%v became %d, want %d", c.err, rec.Code, c.want)
		}
	}
}

// The whole point of the module: an app attached to a database receives
// the connection string, without anybody typing one.
//
// Checked through the app's own env endpoint rather than the
// datastore's, because that endpoint is what the deploy builds a
// container from — and it is where somebody goes to find out why an app
// is connecting where it is.
func TestAnAttachedAppInheritsTheConnectionString(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})

	if rec := attach(t, f, "pg", "web/production/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}

	effective := appEnv(t, f, "web/production/api")
	url, ok := effective["DATABASE_URL"]
	if !ok {
		t.Fatalf("the app inherited no DATABASE_URL: %v", effective)
	}
	if url.Source != envvar.SourceDatastore {
		t.Errorf("DATABASE_URL is labelled %q; nobody typed it, so it should say where it came from", url.Source)
	}
	if !strings.Contains(url.Value, "cubeship-db-pg") {
		t.Errorf("DATABASE_URL points at %q, not the datastore's container on the shared network", url.Value)
	}

	// An app's own variable still wins, which is how you point one
	// somewhere else without detaching anything.
	rec := f.Do(t, http.MethodPatch, "/apps/web/production/api/env",
		map[string]any{"set": map[string]string{"DATABASE_URL": "postgresql://elsewhere/db"}}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("set app env: %d %s", rec.Code, rec.Body.String())
	}
	if got := appEnv(t, f, "web/production/api")["DATABASE_URL"]; got.Source != envvar.SourceApp {
		t.Errorf("the app's own DATABASE_URL lost to the datastore's: %+v", got)
	}

	// Detaching takes the rest of them away again.
	rec = f.Do(t, http.MethodDelete, "/datastores/pg/attachments/web/production/api", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", rec.Code, rec.Body.String())
	}
	if _, still := appEnv(t, f, "web/production/api")["DATABASE_HOST"]; still {
		t.Error("the app still inherits DATABASE_HOST after being detached")
	}
}

func appEnv(t *testing.T, f *servertest.Fixture, ref string) map[string]envvar.Resolved {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/apps/"+ref+"/env", nil, f.AdminKey)
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

// The reason a datastore belongs to the instance rather than to a
// project: on one host, one Postgres serving several small apps is the
// normal shape, and those apps are routinely in different projects.
//
// Owned by a project, this was not merely awkward — it was refused.
func TestOneDatabaseServesAppsInDifferentProjects(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, "/projects", map[string]any{"slug": "blog"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create the second project: %d %s", rec.Code, rec.Body.String())
	}
	createApp(t, f, "web", "production", "api")
	createApp(t, f, "blog", "production", "site")
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})

	for _, ref := range []string{"web/production/api", "blog/production/site"} {
		if rec := attach(t, f, "pg", ref, ""); rec.Code != http.StatusCreated {
			t.Fatalf("attach %s: %d %s", ref, rec.Code, rec.Body.String())
		}
		if _, ok := appEnv(t, f, ref)["DATABASE_URL"]; !ok {
			t.Errorf("%s inherited no DATABASE_URL", ref)
		}
	}

	// And an environment is no longer a boundary either: staging can be
	// attached to the same database, which is the cost of this shape
	// and the thing a name has to carry now.
	rec = f.Do(t, http.MethodPost, "/projects/web/environments",
		map[string]any{"slug": "staging"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create environment: %d %s", rec.Code, rec.Body.String())
	}
	createApp(t, f, "web", "staging", "api")
	if rec := attach(t, f, "pg", "web/staging/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach staging: %d %s", rec.Code, rec.Body.String())
	}
}

// A database outlives the apps that used it — it is the instance's, and
// deleting an app is not a decision about anybody's data. What goes is
// the wiring, which the foreign key removes.
func TestDeletingAnAppLeavesTheDatabaseAndDropsTheWiring(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	if rec := attach(t, f, "pg", "web/production/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}

	// Deleting the whole project takes the app, which is the widest
	// version of the same question.
	if rec := f.Do(t, http.MethodDelete, "/projects/web", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Fatalf("delete project: %d %s", rec.Code, rec.Body.String())
	}

	rec := f.Do(t, http.MethodGet, "/datastores/pg", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("the database went with the project it was never part of: %d %s", rec.Code, rec.Body.String())
	}
	var d struct {
		Attachments []map[string]any `json:"attachments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d.Attachments) != 0 {
		t.Errorf("the attachment survived the app it named: %v", d.Attachments)
	}
}

// The password is stored as given, because a hash cannot connect to
// anything — so the only thing keeping it from leaking is that no
// listing carries it.
func TestThePasswordIsNeverInAListing(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	created := createDatastore(t, f, map[string]any{
		"name": "pg", "engine": "postgres", "password": "hunter2-and-then-some",
	})
	// Create is the one place it comes back, because a caller who left
	// the field empty has to be told what was generated.
	if created["password"] != "hunter2-and-then-some" {
		t.Errorf("create did not report the password: %v", created["password"])
	}

	for _, path := range []string{"/datastores", "/datastores/pg"} {
		rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "hunter2") {
			t.Errorf("GET %s carries the password: %s", path, rec.Body.String())
		}
	}

	// It is readable, deliberately and on its own request.
	rec := f.Do(t, http.MethodGet, "/datastores/pg/credentials", nil, f.AdminKey)
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

// A database's series is served at the database's own address. Same
// module behind it as an app's — what differs is how the subject is
// resolved, and that is what this pins.
func TestADatabasesMetricsAreAMembersToRead(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodGet, "/datastores/pg/metrics", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read metrics as a member: %d %s", rec.Code, rec.Body.String())
	}
	var series struct {
		Window  string           `json:"window"`
		Samples []map[string]any `json:"samples"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if series.Window != "1h" || series.Samples == nil {
		t.Errorf("window %q, samples %v", series.Window, series.Samples)
	}

	rec = f.Do(t, http.MethodGet, "/datastores/pg/metrics?window=6h", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("a window this release offers: %d %s", rec.Code, rec.Body.String())
	}
	rec = f.Do(t, http.MethodGet, "/datastores/nope/metrics", nil, f.AdminKey)
	if rec.Code != http.StatusNotFound {
		t.Errorf("metrics for an unknown database: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// Running a database costs disk on this host and its password reaches
// every app attached to it, so every write is an admin's. Reading which
// databases exist is not — you cannot understand what you are deploying
// into without it.
func TestOnlyAnAdminManagesDatabases(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	refused := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/datastores", map[string]any{"name": "other", "engine": "postgres"}},
		{http.MethodPatch, "/datastores/pg", map[string]any{"description": "mine now"}},
		{http.MethodDelete, "/datastores/pg", nil},
		{http.MethodGet, "/datastores/pg/credentials", nil},
		{http.MethodPost, "/datastores/pg/expose", map[string]any{}},
		{http.MethodDelete, "/datastores/pg/expose", nil},
		{http.MethodPost, "/datastores/pg/attachments", map[string]any{"app": "web/production/api"}},
		{http.MethodDelete, "/datastores/pg/attachments/web/production/api", nil},
	}
	for _, c := range refused {
		rec := f.Do(t, c.method, c.path, c.body, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d %s, want 403", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	for _, path := range []string{"/datastores", "/datastores/engines", "/datastores/pg"} {
		rec := f.Do(t, http.MethodGet, path, nil, memberKey)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s as a member: %d %s, want 200", path, rec.Code, rec.Body.String())
		}
	}
}

// An app and a database are two different kinds of thing, named by two
// different people at two different times. Their containers are in
// separate namespaces, so refusing the second name would be a rule with
// no reason behind it.
func TestAnAppAndADatabaseMayShareAName(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "api", "engine": "postgres"})
}

// The name is the container, so two databases cannot share one — and
// the API's own path segment is not available either.
func TestANameIsUniqueOnTheInstanceAndNotTheApisOwn(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})

	rec := f.Do(t, http.MethodPost, "/datastores",
		map[string]any{"name": "pg", "engine": "mysql"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a second database took the name: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores",
		map[string]any{"name": "engines", "engine": "postgres"}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf(`"engines" was accepted, and nothing could then fetch it: %d %s`, rec.Code, rec.Body.String())
	}
}

// The version is what wrote the data directory, and no other version of
// the same engine can read it. A PATCH that quietly accepted one would
// be a container that will not start with the only copy of the data
// inside the directory it will not read.
func TestNeitherEngineNorVersionCanBeChanged(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	before := createDatastore(t, f, map[string]any{
		"name": "pg", "engine": "postgres", "version": "16",
	})

	rec := f.Do(t, http.MethodPatch, "/datastores/pg", map[string]any{
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
		{"name": "a", "engine": "cockroach"},
		{"name": "b", "engine": "postgres", "version": "99"},
		{"name": "c", "engine": "mysql", "username": "root"},
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
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "my", "engine": "mysql"})

	rec := f.Do(t, http.MethodPost, "/datastores/pg/expose", map[string]any{}, f.AdminKey)
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

	rec = f.Do(t, http.MethodPost, "/datastores/my/expose",
		map[string]any{"port": exposed.Port}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a second datastore took the same port: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores/my/expose",
		map[string]any{"port": 80}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("port 80 was accepted: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodDelete, "/datastores/pg/expose", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpose: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "exposed_port") {
		t.Errorf("still reports a port after being unexposed: %s", rec.Body.String())
	}
}

// One app, two databases, and the prefix is what keeps them from being
// one variable with two values.
func TestASecondDatabaseOnOneAppNeedsAPrefix(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "main", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "analytics", "engine": "postgres"})

	if rec := attach(t, f, "main", "web/production/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach the first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := attach(t, f, "analytics", "web/production/api", ""); rec.Code != http.StatusConflict {
		t.Errorf("a second database landed on the same variables: %d %s", rec.Code, rec.Body.String())
	}
	if rec := attach(t, f, "analytics", "web/production/api", "ANALYTICS_"); rec.Code != http.StatusCreated {
		t.Fatalf("attach with a prefix: %d %s", rec.Code, rec.Body.String())
	}

	env := appEnv(t, f, "web/production/api")
	for _, key := range []string{"DATABASE_URL", "ANALYTICS_DATABASE_URL"} {
		if _, ok := env[key]; !ok {
			t.Errorf("the app did not inherit %s", key)
		}
	}
	if env["DATABASE_URL"].Value == env["ANALYTICS_DATABASE_URL"].Value {
		t.Error("both databases resolved to the same connection string")
	}
}

// fakeDocker is a Docker that agrees to everything, so a test can watch
// a datastore go up, off and back on. servertest's own stub refuses
// every call, which is right for the tests that must not deploy by
// accident and useless for these.
type fakeDocker struct {
	mu      sync.Mutex
	running map[string]bool
	stopped []string
}

func newFakeDocker() *fakeDocker { return &fakeDocker{running: map[string]bool{}} }

func (f *fakeDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }

func (f *fakeDocker) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return opts.Name + "-id", nil
}

func (f *fakeDocker) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[id] = true
	return nil
}

func (f *fakeDocker) StopContainer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, name)
	f.running[name+"-id"] = false
	return nil
}

func (f *fakeDocker) RemoveContainer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.running, name+"-id")
	return nil
}

func (f *fakeDocker) IsRunning(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id], nil
}

func (f *fakeDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	// Docker frames stdout and stderr behind an 8-byte header; the
	// handler demultiplexes it, so a fake that returns raw bytes would
	// be testing the wrong thing.
	var framed bytes.Buffer
	line := []byte("database system is ready to accept connections\n")
	header := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	header[4] = byte(len(line) >> 24)
	header[5] = byte(len(line) >> 16)
	header[6] = byte(len(line) >> 8)
	header[7] = byte(len(line))
	framed.Write(header)
	framed.Write(line)
	return io.NopCloser(&framed), nil
}

// provisioned creates a datastore against a Docker that works and waits
// for it to come up.
func provisioned(t *testing.T, name string) (*servertest.Fixture, *fakeDocker) {
	t.Helper()
	docker := newFakeDocker()
	f := servertest.NewWithDocker(t, docker)
	// The provisioner watches a started container for a while before
	// calling it up, which is right on a real box and nine seconds of
	// nothing here.
	f.Server.Datastores.Provisioner().ReadyInterval = time.Millisecond

	createDatastore(t, f, map[string]any{"name": name, "engine": "postgres"})
	f.Server.Datastores.WaitForProvisioning()
	return f, docker
}

func statusOf(t *testing.T, f *servertest.Fixture, name string) string {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/datastores/"+name, nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("get datastore: %d %s", rec.Code, rec.Body.String())
	}
	var d struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return d.Status
}

// Turning a database off is a decision, and it has to keep looking like
// one. "down" is a container that stopped on its own; a screen that
// showed the same word for both would send somebody looking for a fault
// they created on purpose.
func TestStoppingADatabaseIsNotTheSameAsItFallingOver(t *testing.T) {
	dbtest.RequireDatabase(t)
	f, docker := provisioned(t, "pg")

	if got := statusOf(t, f, "pg"); got != "running" {
		t.Fatalf("a freshly provisioned datastore is %q", got)
	}

	rec := f.Do(t, http.MethodPost, "/datastores/pg/stop", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}
	if got := statusOf(t, f, "pg"); got != "stopped" {
		t.Errorf("after stopping, the datastore is %q, want stopped", got)
	}
	if len(docker.stopped) != 1 || docker.stopped[0] != "cubeship-db-pg" {
		t.Errorf("stopped %v, want the container by name", docker.stopped)
	}

	// The reconciler runs at every daemon start and sees a container
	// that is not running. Rewriting that to "down" would turn the
	// decision into what looks like a fault, on every restart.
	if err := datastore.Reconcile(t.Context(), f.Server.Datastores.Repo(), docker); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := statusOf(t, f, "pg"); got != "stopped" {
		t.Errorf("the reconciler rewrote a deliberate stop to %q", got)
	}

	// And back on. It is provisioned again rather than restarted, which
	// is one path instead of two.
	rec = f.Do(t, http.MethodPost, "/datastores/pg/start", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
	if got := statusOf(t, f, "pg"); got != "running" {
		t.Errorf("after starting, the datastore is %q, want running", got)
	}
}

// Turning a database off takes it away from every app attached to it,
// so it is an admin's. Reading its log is not: the reason it is
// refusing connections is in there, and finding that out is not a
// privilege.
func TestLogsAreAMembersAndStoppingIsAnAdmins(t *testing.T) {
	dbtest.RequireDatabase(t)
	f, _ := provisioned(t, "pg")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodGet, "/datastores/pg/logs", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read logs as a member: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ready to accept connections") {
		t.Errorf("logs came back as %q — the frame header was not demultiplexed", rec.Body.String())
	}

	for _, path := range []string{"/datastores/pg/stop", "/datastores/pg/start"} {
		if rec := f.Do(t, http.MethodPost, path, nil, memberKey); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s as a member: %d, want 403", path, rec.Code)
		}
	}
}

// There is nothing to stop or to read a log from before a container
// exists, and saying so beats a 500 out of the Engine.
func TestStoppingAndLoggingNeedAContainer(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	f.Server.Datastores.WaitForProvisioning()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/datastores/pg/stop"},
		{http.MethodGet, "/datastores/pg/logs"},
	} {
		rec := f.Do(t, c.method, c.path, nil, f.AdminKey)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s %s: %d %s, want 409", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	// Starting is how a failed provision is retried, so it is allowed.
	if rec := f.Do(t, http.MethodPost, "/datastores/pg/start", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Errorf("start after a failed provision: %d %s", rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
}
