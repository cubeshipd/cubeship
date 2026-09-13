package templateinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/user"
)

const umami = `version: 1
minCubeship: "0.6.0"
project: umami
inputs:
  - key: domain
    type: domain
    label: Where it answers
  - key: appSecret
    type: secret
    label: Session secret
    generate: 32
databases:
  - key: db
    name: umami-db
    engine: postgres
    version: "18"
    database: umami
apps:
  - key: web
    name: web
    image: ghcr.io/umami-software/umami
    tag: "3.3.1"
    port: 3000
    health: /api/heartbeat
    domains:
      - host: ${input.domain}
    attach:
      - database: db
    limits: { cpu: 1, memory: 1Gi }
    env:
      APP_SECRET: ${input.appSecret}
      DB_HOST: ${db.db.host}
      SELF: http://${app.web.internal}:${app.web.port}
`

var admin = &user.User{ID: 1, Username: "admin", Role: user.RoleAdmin}

// world is every fake module at once, with one log of what was done to
// it, in order — which is what a test about undoing has to read.
type world struct {
	mu  sync.Mutex
	log []string

	projects map[string][]string // slug → environments
	apps     map[string]bool     // reference
	dbs      map[string]bool
	stores   map[string]bool
	hosts    map[string]bool

	env         map[string]envvar.Map
	updates     map[string]app.Limits
	specs       []datastore.Spec
	deployFails bool
}

func newWorld() *world {
	return &world{
		projects: map[string][]string{}, apps: map[string]bool{}, dbs: map[string]bool{},
		stores: map[string]bool{}, hosts: map[string]bool{},
		env: map[string]envvar.Map{}, updates: map[string]app.Limits{},
	}
}

func (w *world) did(event string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.log = append(w.log, event)
}

func (w *world) events() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.log)
}

type fakeProjects struct{ *world }

func (f fakeProjects) Resolve(_ context.Context, _ *user.User, slug string, _ user.Role) (*project.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[slug]; !ok {
		return nil, project.ErrNotFound
	}
	return &project.Project{Slug: slug}, nil
}

func (f fakeProjects) ResolveEnvironment(_ context.Context, _ *user.User, slug, env string, _ user.Role) (*project.Environment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.projects[slug], env) {
		return nil, project.ErrEnvironmentNotFound
	}
	return &project.Environment{Slug: env}, nil
}

func (f fakeProjects) Create(_ context.Context, _ *user.User, slug string) (*project.Project, *project.Environment, error) {
	f.mu.Lock()
	f.projects[slug] = []string{project.ProductionEnvSlug}
	f.mu.Unlock()
	f.did("create project " + slug)
	return &project.Project{Slug: slug}, &project.Environment{Slug: project.ProductionEnvSlug}, nil
}

func (f fakeProjects) CreateEnvironment(_ context.Context, _ *user.User, slug, env string) (*project.Environment, error) {
	f.mu.Lock()
	f.projects[slug] = append(f.projects[slug], env)
	f.mu.Unlock()
	f.did("create environment " + slug + "/" + env)
	return &project.Environment{Slug: env}, nil
}

func (f fakeProjects) Delete(_ context.Context, _ *user.User, slug string) (*project.Project, error) {
	f.did("delete project " + slug)
	return &project.Project{Slug: slug}, nil
}

func (f fakeProjects) DeleteEnvironment(_ context.Context, _ *user.User, slug, env string) (*project.Environment, error) {
	f.did("delete environment " + slug + "/" + env)
	return &project.Environment{Slug: env}, nil
}

type fakeApps struct{ *world }

func (f fakeApps) Resolve(_ context.Context, _ *user.User, ref app.Reference, _ user.Role) (*app.Scoped, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.apps[ref.String()] {
		return nil, app.ErrNotFound
	}
	return &app.Scoped{}, nil
}

func (f fakeApps) Create(_ context.Context, _ *user.User, projectSlug, envSlug, name string, source app.Source, origin app.Origin) (*app.Scoped, error) {
	ref := projectSlug + "/" + envSlug + "/" + name
	f.mu.Lock()
	f.apps[ref] = true
	f.mu.Unlock()
	f.did("create app " + ref + " " + string(source) + " " + origin.Image + ":" + origin.Tag)
	return &app.Scoped{}, nil
}

func (f fakeApps) Update(_ context.Context, _ *user.User, ref app.Reference, _ *app.Source, _ *app.Origin, health *string, limits *app.Limits, _ *app.Autoscale, _ *app.Placement) (*app.Scoped, error) {
	f.mu.Lock()
	if limits != nil {
		f.updates[ref.String()] = *limits
	}
	f.mu.Unlock()
	f.did("configure app " + ref.String() + " health " + deref(health))
	return &app.Scoped{}, nil
}

func (f fakeApps) AddDomain(_ context.Context, _ *user.User, ref app.Reference, host string, port int) (*app.Scoped, error) {
	f.did("add domain " + host + " to " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) MergeEnv(_ context.Context, _ *user.User, ref app.Reference, set envvar.Map, _ []string) (*app.Scoped, error) {
	f.mu.Lock()
	f.env[ref.String()] = set
	f.mu.Unlock()
	f.did("set variables on " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) Deploy(_ context.Context, _ *user.User, ref app.Reference, _ string) (*app.Scoped, *app.Deployment, error) {
	f.did("deploy " + ref.String())
	return &app.Scoped{}, &app.Deployment{ID: 7, Status: app.DeploymentPending}, nil
}

func (f fakeApps) WaitForDeployment(_ context.Context, _ *user.User, _ app.Reference, id int64) (*app.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deployFails {
		return &app.Deployment{ID: id, Status: app.DeploymentFailed, Error: "container exited with status 1"}, nil
	}
	return &app.Deployment{ID: id, Status: app.DeploymentSucceeded}, nil
}

func (f fakeApps) Delete(_ context.Context, _ *user.User, ref app.Reference) (*app.Scoped, error) {
	f.did("delete app " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) HostTaken(_ context.Context, host string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hosts[host], nil
}

type fakeDatastores struct{ *world }

func (f fakeDatastores) Resolve(_ context.Context, _ *user.User, name string, _ user.Role) (*datastore.Datastore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.dbs[name] {
		return nil, datastore.ErrNotFound
	}
	return &datastore.Datastore{Slug: name, Status: datastore.StatusRunning}, nil
}

func (f fakeDatastores) Create(_ context.Context, _ *user.User, spec datastore.Spec) (*datastore.Datastore, error) {
	f.mu.Lock()
	f.dbs[spec.Slug] = true
	f.specs = append(f.specs, spec)
	f.mu.Unlock()
	f.did("create database " + spec.Slug)
	return &datastore.Datastore{Slug: spec.Slug, Status: datastore.StatusProvisioning}, nil
}

func (f fakeDatastores) Credentials(_ context.Context, _ *user.User, name string) (datastore.Credentials, error) {
	return datastore.Credentials{Username: "cubeship", Password: "pw", Database: "umami",
		InternalHost: "cubeship-db-" + name, InternalPort: 5432}, nil
}

func (f fakeDatastores) Attach(_ context.Context, _ *user.User, name, appRef, _ string) (*datastore.Datastore, error) {
	f.did("attach " + name + " to " + appRef)
	return &datastore.Datastore{Slug: name}, nil
}

func (f fakeDatastores) Delete(_ context.Context, _ *user.User, name string) (*datastore.Datastore, error) {
	f.did("delete database " + name)
	return &datastore.Datastore{Slug: name}, nil
}

type fakeStores struct{ *world }

func (f fakeStores) Resolve(_ context.Context, _ *user.User, name string, _ user.Role) (*objectstore.Store, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stores[name] {
		return nil, objectstore.ErrNotFound
	}
	return &objectstore.Store{Slug: name, Status: objectstore.StatusRunning}, nil
}

func (f fakeStores) Create(_ context.Context, _ *user.User, spec objectstore.ManagedSpec) (*objectstore.Store, error) {
	f.mu.Lock()
	f.stores[spec.Slug] = true
	f.mu.Unlock()
	f.did("create store " + spec.Slug)
	return &objectstore.Store{Slug: spec.Slug}, nil
}

func (f fakeStores) Credentials(context.Context, *user.User, string) (objectstore.Credentials, error) {
	return objectstore.Credentials{Endpoint: "http://minio:9000"}, nil
}

func (f fakeStores) CreateBucket(_ context.Context, _ *user.User, name, bucket string) error {
	f.did("create bucket " + bucket + " in " + name)
	return nil
}

func (f fakeStores) Attach(_ context.Context, _ *user.User, name, appRef, bucket, _ string) (*objectstore.Store, error) {
	f.did("attach " + name + "/" + bucket + " to " + appRef)
	return &objectstore.Store{Slug: name}, nil
}

func (f fakeStores) Delete(_ context.Context, _ *user.User, name string) (*objectstore.Store, error) {
	f.did("delete store " + name)
	return &objectstore.Store{Slug: name}, nil
}

type fakeUsers struct{}

func (fakeUsers) ByID(_ context.Context, id int64) (*user.User, error) {
	if id == admin.ID {
		return admin, nil
	}
	return nil, errors.New("no such user")
}

type fakeRecords struct {
	mu   sync.Mutex
	rows map[int64]Install
}

func (r *fakeRecords) Create(_ context.Context, in *Install) (*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	created := *in
	created.ID = int64(len(r.rows) + 1)
	created.CreatedAt = time.Now()
	r.rows[created.ID] = created
	return &created, nil
}

func (r *fakeRecords) Save(_ context.Context, in *Install) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	saved := *in
	saved.Resources = slices.Clone(in.Resources)
	r.rows[in.ID] = saved
	return nil
}

func (r *fakeRecords) ByID(_ context.Context, id int64) (*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &row, nil
}

func (r *fakeRecords) List(context.Context, int) ([]*Install, error) { return nil, nil }

func (r *fakeRecords) Running(context.Context) ([]*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Install
	for _, row := range r.rows {
		if row.Status == StatusRunning {
			copied := row
			out = append(out, &copied)
		}
	}
	return out, nil
}

type fakeCatalog struct{ source string }

func (c fakeCatalog) List(context.Context, url.Values) (json.RawMessage, error) { return nil, nil }
func (c fakeCatalog) Template(context.Context, string, string) (json.RawMessage, error) {
	return nil, nil
}
func (c fakeCatalog) Icon(context.Context, string, string) ([]byte, error) { return nil, nil }

func (c fakeCatalog) Releases(context.Context, string, string) ([]CatalogRelease, error) {
	return []CatalogRelease{
		{Tag: "v1.2.0", Commit: "bad0000", Status: "rejected"},
		{Tag: "v1.1.0", Commit: "abc1234", Status: "accepted"},
	}, nil
}

func (c fakeCatalog) Source(_ context.Context, _, _, commit string) ([]byte, error) {
	if commit != "abc1234" {
		return nil, ErrReleaseNotFound
	}
	return []byte(c.source), nil
}

func newService(w *world, records *fakeRecords, version string) *Service {
	s := NewService(records, fakeProjects{w}, fakeApps{w}, fakeDatastores{w}, fakeStores{w}, fakeUsers{},
		fakeCatalog{source: umami}, version)
	s.Poll = time.Millisecond
	return s
}

func install(t *testing.T, s *Service, records *fakeRecords, req Request) (*Install, map[string]string) {
	t.Helper()
	started, secrets, err := s.Install(context.Background(), admin, req)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	s.Wait()
	done, err := records.ByID(context.Background(), started.ID)
	if err != nil {
		t.Fatal(err)
	}
	return done, secrets
}

func umamiRequest() Request {
	return Request{Owner: "cubeshipd", Repo: "cubeship-umami-template",
		Inputs: map[string]string{"domain": "Analytics.Example.com"}}
}

func TestAnInstallCreatesWhatTheTemplateDeclaresAndDeploysIt(t *testing.T) {
	w := newWorld()
	records := &fakeRecords{rows: map[int64]Install{}}
	s := newService(w, records, "0.7.0")

	done, secrets := install(t, s, records, umamiRequest())

	if done.Status != StatusSucceeded || done.Error != "" || done.Release != "v1.1.0" || done.Commit != "abc1234" {
		t.Fatalf("install = %+v", done)
	}
	want := []Resource{{KindProject, "umami"}, {KindDatabase, "umami-db"}, {KindApp, "umami/production/web"}}
	if !slices.Equal(done.Resources, want) {
		t.Errorf("resources = %v, want %v", done.Resources, want)
	}
	if len(secrets["appSecret"]) != 32 {
		t.Fatalf("generated secret = %q", secrets["appSecret"])
	}
	env := w.env["umami/production/web"]
	if env["APP_SECRET"] != secrets["appSecret"] || env["DB_HOST"] != "cubeship-db-umami-db" ||
		env["SELF"] != "http://cubeship-umami-production-web:3000" {
		t.Errorf("variables = %v", env)
	}
	if limits := w.updates["umami/production/web"]; limits.CPU != 1 || limits.Memory != 1<<30 {
		t.Errorf("limits = %+v", limits)
	}
	if spec := w.specs[0]; spec.Engine != datastore.EnginePostgres || spec.Version != "18" || spec.Database != "umami" {
		t.Errorf("database spec = %+v", spec)
	}
	for _, event := range []string{
		"create app umami/production/web external ghcr.io/umami-software/umami:3.3.1",
		"add domain analytics.example.com to umami/production/web",
		"attach umami-db to umami/production/web",
		"deploy umami/production/web",
	} {
		if !slices.Contains(w.events(), event) {
			t.Errorf("nothing did %q in %v", event, w.events())
		}
	}
}

func TestAFailedDeployUndoesEverythingTheInstallCreatedNewestFirst(t *testing.T) {
	w := newWorld()
	w.deployFails = true
	records := &fakeRecords{rows: map[int64]Install{}}
	s := newService(w, records, "0.7.0")

	done, _ := install(t, s, records, umamiRequest())

	if done.Status != StatusFailed || !strings.Contains(done.Error, "deploy app umami/production/web failed: container exited with status 1") {
		t.Fatalf("install = %+v", done)
	}
	events := w.events()
	undone := events[len(events)-3:]
	if !slices.Equal(undone, []string{"delete app umami/production/web", "delete database umami-db", "delete project umami"}) {
		t.Errorf("undone = %v", undone)
	}
}

func TestAnInstallIntoAnExistingProjectLeavesThatProjectAlone(t *testing.T) {
	w := newWorld()
	w.projects["umami"] = []string{project.ProductionEnvSlug}
	w.deployFails = true
	records := &fakeRecords{rows: map[int64]Install{}}
	s := newService(w, records, "0.7.0")

	done, _ := install(t, s, records, umamiRequest())

	if slices.Contains(done.Resources, Resource{KindProject, "umami"}) {
		t.Errorf("the existing project was recorded as created: %v", done.Resources)
	}
	for _, event := range w.events() {
		if strings.HasPrefix(event, "delete project") || strings.HasPrefix(event, "create project") {
			t.Errorf("the existing project was touched: %s", event)
		}
	}
}

func TestANewEnvironmentInAnExistingProjectIsCreatedAndUndone(t *testing.T) {
	w := newWorld()
	w.projects["umami"] = []string{project.ProductionEnvSlug}
	w.deployFails = true
	records := &fakeRecords{rows: map[int64]Install{}}
	s := newService(w, records, "0.7.0")

	req := umamiRequest()
	req.Environment = "staging"
	install(t, s, records, req)

	events := w.events()
	if !slices.Contains(events, "create environment umami/staging") || events[len(events)-1] != "delete environment umami/staging" {
		t.Errorf("events = %v", events)
	}
}

func TestRefusalsCreateNothing(t *testing.T) {
	for name, c := range map[string]struct {
		prepare func(w *world, req *Request)
		version string
		check   func(error) bool
	}{
		"a database name already taken": {
			prepare: func(w *world, _ *Request) { w.dbs["umami-db"] = true },
			check:   func(err error) bool { var taken *TakenError; return errors.As(err, &taken) && taken.Name == "umami-db" },
		},
		"a renamed database that is free": {
			prepare: func(w *world, req *Request) {
				w.dbs["umami-db"] = true
				req.Databases = map[string]string{"db": "analytics-db"}
				req.Inputs["domain"] = "not a host"
			},
			check: func(err error) bool {
				var input *InputError
				return errors.As(err, &input) && input.Key == "inputs.domain"
			},
		},
		"a domain another app answers at": {
			prepare: func(w *world, _ *Request) { w.hosts["analytics.example.com"] = true },
			check:   func(err error) bool { var taken *TakenError; return errors.As(err, &taken) && taken.Kind == "domain" },
		},
		"a required input left out": {
			prepare: func(_ *world, req *Request) { req.Inputs = nil },
			check: func(err error) bool {
				var input *InputError
				return errors.As(err, &input) && input.Key == "inputs.domain"
			},
		},
		"an app name that is not a slug": {
			prepare: func(_ *world, req *Request) { req.Apps = map[string]string{"web": "Web App"} },
			check:   func(err error) bool { var input *InputError; return errors.As(err, &input) && input.Key == "apps.web" },
		},
		"a release the catalog did not accept": {
			prepare: func(_ *world, req *Request) { req.Release = "v1.2.0" },
			check:   func(err error) bool { return errors.Is(err, ErrReleaseNotFound) },
		},
		"an instance older than the template asks for": {
			prepare: func(*world, *Request) {},
			version: "0.5.2",
			check:   func(err error) bool { return errors.Is(err, ErrTooNew) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld()
			records := &fakeRecords{rows: map[int64]Install{}}
			version := c.version
			if version == "" {
				version = "0.7.0"
			}
			s := newService(w, records, version)
			req := umamiRequest()
			c.prepare(w, &req)

			_, _, err := s.Install(context.Background(), admin, req)
			s.Wait()
			if err == nil || !c.check(err) {
				t.Fatalf("err = %v", err)
			}
			if len(records.rows) != 0 || len(w.events()) != 0 {
				t.Errorf("a refused install recorded %v and did %v", records.rows, w.events())
			}
		})
	}
}

func TestOnlyAnAdminInstalls(t *testing.T) {
	w := newWorld()
	records := &fakeRecords{rows: map[int64]Install{}}
	s := newService(w, records, "0.7.0")
	member := &user.User{ID: 2, Role: user.RoleMember}
	if _, _, err := s.Install(context.Background(), member, umamiRequest()); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("err = %v", err)
	}
}

func TestRecoverUndoesAnInstallAPreviousDaemonLeftRunning(t *testing.T) {
	w := newWorld()
	records := &fakeRecords{rows: map[int64]Install{
		1: {ID: 1, Status: StatusRunning, CreatedBy: admin.ID,
			Resources: []Resource{{KindProject, "umami"}, {KindDatabase, "umami-db"}}},
		2: {ID: 2, Status: StatusRunning,
			Resources: []Resource{{KindDatabase, "orphan-db"}}},
	}}
	s := newService(w, records, "0.7.0")

	if err := s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.events(), []string{"delete database umami-db", "delete project umami"}) {
		t.Errorf("events = %v", w.events())
	}
	for id, row := range records.rows {
		if row.Status != StatusFailed || row.Error == "" {
			t.Errorf("install %d = %+v", id, row)
		}
	}
}

func TestGeneratedSecretsAreLettersAndDigits(t *testing.T) {
	secret, err := generateSecret(64)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 64 || strings.Trim(secret, secretAlphabet) != "" {
		t.Errorf("secret = %q", secret)
	}
}
