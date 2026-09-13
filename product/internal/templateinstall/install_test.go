package templateinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
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

// umamiNext is the next release: a newer image, a variable that needs the
// secret the first release generated, a question it did not ask, a cache,
// and SELF gone.
const umamiNext = `version: 1
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
  - key: timezone
    type: text
    label: Time zone
databases:
  - key: db
    name: umami-db
    engine: postgres
    version: "18"
    database: umami
  - key: cache
    name: umami-cache
    engine: redis
    version: "7.4"
apps:
  - key: web
    name: web
    image: ghcr.io/umami-software/umami
    tag: "3.4.0"
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
      SESSION_KEY: ${input.appSecret}
      TZ: ${input.timezone}
`

var admin = &user.User{ID: 1, Username: "admin", Role: user.RoleAdmin}

// world is every fake module at once, with one log of what was done to
// it, in order — which is what a test about undoing has to read.
type world struct {
	mu  sync.Mutex
	log []string

	projects map[string][]string // slug → environments
	apps     map[string]*app.Scoped
	dbs      map[string]bool
	stores   map[string]bool
	hosts    map[string]bool

	specs       []datastore.Spec
	deployFails bool
}

func newWorld() *world {
	return &world{
		projects: map[string][]string{}, apps: map[string]*app.Scoped{}, dbs: map[string]bool{},
		stores: map[string]bool{}, hosts: map[string]bool{},
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

func (w *world) app(ref string) app.Scoped {
	w.mu.Lock()
	defer w.mu.Unlock()
	a := *w.apps[ref]
	a.Env = maps.Clone(a.Env)
	return a
}

type fakeProjects struct{ *world }

func (f fakeProjects) Resolve(_ context.Context, _ *user.User, slug string, _ user.Level) (*project.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.projects[slug]; !ok {
		return nil, project.ErrNotFound
	}
	return &project.Project{Slug: slug}, nil
}

func (f fakeProjects) ResolveEnvironment(_ context.Context, _ *user.User, slug, env string, _ user.Level) (*project.Environment, error) {
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
	f.mu.Lock()
	delete(f.projects, slug)
	f.mu.Unlock()
	f.did("delete project " + slug)
	return &project.Project{Slug: slug}, nil
}

func (f fakeProjects) SetImage(_ context.Context, _ *user.User, slug string, body io.Reader) error {
	data, _ := io.ReadAll(body)
	f.did("set image " + slug + " " + string(data))
	return nil
}

func (f fakeProjects) DeleteEnvironment(_ context.Context, _ *user.User, slug, env string) (*project.Environment, error) {
	f.did("delete environment " + slug + "/" + env)
	return &project.Environment{Slug: env}, nil
}

type fakeApps struct{ *world }

func (f fakeApps) Resolve(_ context.Context, _ *user.User, ref app.Reference, _ user.Level) (*app.Scoped, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.apps[ref.String()]
	if !ok {
		return nil, app.ErrNotFound
	}
	c := *a
	return &c, nil
}

func (f fakeApps) Create(_ context.Context, _ *user.User, projectSlug, envSlug, name string, source app.Source, origin app.Origin) (*app.Scoped, error) {
	ref := projectSlug + "/" + envSlug + "/" + name
	f.mu.Lock()
	f.apps[ref] = &app.Scoped{
		App:         app.App{Name: name, Source: string(source), SourceImage: origin.Image, SourceTag: origin.Tag, Env: envvar.Map{}},
		ProjectSlug: projectSlug, EnvironmentSlug: envSlug,
	}
	f.mu.Unlock()
	f.did("create app " + ref + " " + string(source) + " " + origin.Image + ":" + origin.Tag)
	return &app.Scoped{}, nil
}

func (f fakeApps) Update(_ context.Context, _ *user.User, ref app.Reference, source *app.Source, origin *app.Origin, health *string, limits *app.Limits, _ *app.Autoscale, _ *app.Placement) (*app.Scoped, error) {
	f.mu.Lock()
	a := f.apps[ref.String()]
	if origin != nil {
		a.Source, a.SourceImage, a.SourceTag = string(*source), origin.Image, origin.Tag
	}
	if limits != nil {
		a.Limits = *limits
	}
	if health != nil {
		a.HealthPath = *health
	}
	f.mu.Unlock()
	if origin != nil {
		f.did("set source of " + ref.String() + " to " + origin.Image + ":" + origin.Tag)
	}
	return &app.Scoped{}, nil
}

func (f fakeApps) AddDomain(_ context.Context, _ *user.User, ref app.Reference, host string, port int) (*app.Scoped, error) {
	f.mu.Lock()
	a := f.apps[ref.String()]
	a.Domains = append(a.Domains, app.Domain{ID: int64(len(a.Domains) + 1), Host: host, Port: port})
	f.mu.Unlock()
	f.did("add domain " + host + " to " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) RemoveDomain(_ context.Context, _ *user.User, ref app.Reference, id int64) (*app.Scoped, error) {
	f.did("remove a domain from " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) MergeEnv(_ context.Context, _ *user.User, ref app.Reference, set envvar.Map, unset []string) (*app.Scoped, error) {
	f.mu.Lock()
	a := f.apps[ref.String()]
	maps.Copy(a.Env, set)
	for _, name := range unset {
		delete(a.Env, name)
	}
	f.mu.Unlock()
	f.did("set variables on " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) Env(_ context.Context, _ *user.User, ref app.Reference) (envvar.Map, []envvar.Resolved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.apps[ref.String()].Env), nil, nil
}

func (f fakeApps) List(context.Context, *user.User) ([]*app.Scoped, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*app.Scoped
	for _, a := range f.apps {
		c := *a
		out = append(out, &c)
	}
	return out, nil
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

func (f fakeApps) DeleteApp(_ context.Context, _ *user.User, ref app.Reference, deleteVolumeData bool) (*app.Scoped, error) {
	f.mu.Lock()
	hadVolumes := f.apps[ref.String()] != nil && len(f.apps[ref.String()].Volumes) > 0
	delete(f.apps, ref.String())
	f.mu.Unlock()
	if deleteVolumeData && hadVolumes {
		f.did("delete app " + ref.String() + " and its volume data")
	} else {
		f.did("delete app " + ref.String())
	}
	return &app.Scoped{}, nil
}

func (f fakeApps) AddVolume(_ context.Context, _ *user.User, ref app.Reference, containerPath string) (*app.Volume, error) {
	f.mu.Lock()
	a := f.apps[ref.String()]
	v := app.Volume{ID: int64(len(a.Volumes) + 1), Path: containerPath}
	a.Volumes = append(a.Volumes, v)
	f.mu.Unlock()
	f.did("add volume " + containerPath + " to " + ref.String())
	return &v, nil
}

func (f fakeApps) RemoveVolume(_ context.Context, _ *user.User, ref app.Reference, id int64, deleteData bool) error {
	f.did(fmt.Sprintf("remove volume %d from %s", id, ref))
	return nil
}

func (f fakeApps) HostTaken(_ context.Context, host string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hosts[host], nil
}

type fakeDatastores struct{ *world }

func (f fakeDatastores) Resolve(_ context.Context, _ *user.User, name string, _ user.Level) (*datastore.Datastore, error) {
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

func (f fakeDatastores) Detach(_ context.Context, _ *user.User, name, appRef string) (*datastore.Datastore, error) {
	f.did("detach " + name + " from " + appRef)
	return &datastore.Datastore{Slug: name}, nil
}

func (f fakeDatastores) Delete(_ context.Context, _ *user.User, name string) (*datastore.Datastore, error) {
	f.mu.Lock()
	delete(f.dbs, name)
	f.mu.Unlock()
	f.did("delete database " + name)
	return &datastore.Datastore{Slug: name}, nil
}

type fakeStores struct{ *world }

func (f fakeStores) Resolve(_ context.Context, _ *user.User, name string, _ user.Level) (*objectstore.Store, error) {
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

func (f fakeStores) Detach(_ context.Context, _ *user.User, name, appRef, bucket string) (*objectstore.Store, error) {
	f.did("detach " + name + "/" + bucket + " from " + appRef)
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
	mu       sync.Mutex
	installs map[int64]Install
	runs     map[int64]Run
}

func newRecords() *fakeRecords {
	return &fakeRecords{installs: map[int64]Install{}, runs: map[int64]Run{}}
}

func (r *fakeRecords) CreateInstall(_ context.Context, in *Install) (*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *in
	c.ID = int64(len(r.installs) + 1)
	c.CreatedAt = time.Now()
	r.installs[c.ID] = c
	return &c, nil
}

func (r *fakeRecords) SaveInstall(_ context.Context, in *Install) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *in
	c.Resources = slices.Clone(in.Resources)
	c.Answers = maps.Clone(in.Answers)
	r.installs[in.ID] = c
	return nil
}

func (r *fakeRecords) Install(_ context.Context, id int64) (*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.installs[id]
	if !ok {
		return nil, ErrNotFound
	}
	in.Resources = slices.Clone(in.Resources)
	return &in, nil
}

func (r *fakeRecords) Installs(context.Context) ([]*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Install
	for _, in := range r.installs {
		c := in
		out = append(out, &c)
	}
	return out, nil
}

func (r *fakeRecords) CreateRun(_ context.Context, run *Run) (*Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *run
	c.ID = int64(len(r.runs) + 1)
	r.runs[c.ID] = c
	return &c, nil
}

func (r *fakeRecords) SaveRun(_ context.Context, run *Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *run
	c.Created = slices.Clone(run.Created)
	c.Snapshot = slices.Clone(run.Snapshot)
	r.runs[run.ID] = c
	return nil
}

func (r *fakeRecords) Runs(_ context.Context, installID int64) ([]*Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Run
	for _, run := range r.runs {
		if run.InstallID == installID {
			c := run
			out = append(out, &c)
		}
	}
	slices.SortFunc(out, func(a, b *Run) int { return int(b.ID - a.ID) })
	return out, nil
}

func (r *fakeRecords) RunningRuns(context.Context) ([]*Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*Run
	for _, run := range r.runs {
		if run.Status == RunRunning {
			c := run
			out = append(out, &c)
		}
	}
	return out, nil
}

func (r *fakeRecords) run(id int64) Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[id]
}

type fakeCatalog struct {
	mu       *sync.Mutex
	releases *[]CatalogRelease
	sources  map[string]string
}

func newCatalog() fakeCatalog {
	return fakeCatalog{
		mu: &sync.Mutex{},
		releases: &[]CatalogRelease{
			{Tag: "v1.2.0", Commit: "bad0000", Status: "rejected"},
			{Tag: "v1.1.0", Commit: "abc1234", Status: "accepted"},
		},
		sources: map[string]string{"abc1234": umami, "def5678": umamiNext},
	}
}

// publish makes the next release the newest the catalog accepted.
func (c fakeCatalog) publish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.releases = append([]CatalogRelease{{Tag: "v1.3.0", Commit: "def5678", Status: "accepted"}}, *c.releases...)
}

func (c fakeCatalog) List(context.Context, url.Values) (json.RawMessage, error) { return nil, nil }
func (c fakeCatalog) Tags(context.Context) (json.RawMessage, error)             { return nil, nil }

// Template names the newest release's icon, the way HTTPCatalog hands it
// on: rewritten to the instance's own address.
func (c fakeCatalog) Template(context.Context, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"icon_url": "/api/template-icons/4242/def5678.png"}`), nil
}

func (c fakeCatalog) Icon(_ context.Context, repository, file string) ([]byte, error) {
	if repository != "4242" {
		return nil, ErrTemplateNotFound
	}
	return []byte("png of " + file), nil
}

func (c fakeCatalog) Releases(context.Context, string, string) ([]CatalogRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(*c.releases), nil
}

func (c fakeCatalog) Source(_ context.Context, _, _, commit string) ([]byte, error) {
	source, ok := c.sources[commit]
	if !ok {
		return nil, ErrReleaseNotFound
	}
	return []byte(source), nil
}

type fixture struct {
	w       *world
	records *fakeRecords
	catalog fakeCatalog
	s       *Service
}

func newFixture(version string) *fixture {
	f := &fixture{w: newWorld(), records: newRecords(), catalog: newCatalog()}
	f.s = NewService(f.records, fakeProjects{f.w}, fakeApps{f.w}, fakeDatastores{f.w}, fakeStores{f.w}, fakeUsers{},
		f.catalog, version)
	f.s.Poll = time.Millisecond
	return f
}

func umamiRequest() Request {
	return Request{Owner: "cubeshipd", Repo: "cubeship-umami-template",
		Inputs: map[string]string{"domain": "Analytics.Example.com"}}
}

// install runs an install to its end and returns the installation and
// its run as recorded.
func (f *fixture) install(t *testing.T, req Request) (*Install, Run, map[string]string) {
	t.Helper()
	started, secrets, err := f.s.Install(context.Background(), admin, req)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	f.s.Wait()
	in, err := f.records.Install(context.Background(), started.Install.ID)
	if err != nil {
		t.Fatal(err)
	}
	return in, f.records.run(started.Runs[0].ID), secrets
}

func TestAnInstallCreatesWhatTheTemplateDeclaresAndDeploysIt(t *testing.T) {
	f := newFixture("0.7.0")
	in, run, secrets := f.install(t, umamiRequest())

	if in.Status != StatusInstalled || run.Status != RunSucceeded || in.Release != "v1.1.0" || in.Commit != "abc1234" {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	want := []Resource{{KindProject, "", "umami"}, {KindDatabase, "db", "umami-db"}, {KindApp, "web", "umami/production/web"}}
	if !slices.Equal(in.Resources, want) || !slices.Equal(run.Created, want) {
		t.Errorf("resources = %v, created = %v, want %v", in.Resources, run.Created, want)
	}
	if in.Manifest == nil || in.Answers["domain"] != "analytics.example.com" || in.Answers["appSecret"] != "" {
		t.Errorf("recorded manifest %v and answers %v", in.Manifest != nil, in.Answers)
	}
	if len(secrets["appSecret"]) != 32 {
		t.Fatalf("generated secret = %q", secrets["appSecret"])
	}
	web := f.w.app("umami/production/web")
	if web.Env["APP_SECRET"] != secrets["appSecret"] || web.Env["DB_HOST"] != "cubeship-db-umami-db" ||
		web.Env["SELF"] != "http://cubeship-umami-production-web:3000" {
		t.Errorf("variables = %v", web.Env)
	}
	if web.Limits.CPU != 1 || web.Limits.Memory != 1<<30 {
		t.Errorf("limits = %+v", web.Limits)
	}
	if spec := f.w.specs[0]; spec.Engine != datastore.EnginePostgres || spec.Version != "18" || spec.Database != "umami" {
		t.Errorf("database spec = %+v", spec)
	}
	for _, event := range []string{
		"create app umami/production/web external ghcr.io/umami-software/umami:3.3.1",
		"add domain analytics.example.com to umami/production/web",
		"attach umami-db to umami/production/web",
		"deploy umami/production/web",
		// The installed release's icon, not the newest one the listing names.
		"set image umami png of abc1234.png",
	} {
		if !slices.Contains(f.w.events(), event) {
			t.Errorf("nothing did %q in %v", event, f.w.events())
		}
	}
}

func TestAFailedDeployUndoesEverythingTheInstallCreatedNewestFirst(t *testing.T) {
	f := newFixture("0.7.0")
	f.w.deployFails = true
	in, run, _ := f.install(t, umamiRequest())

	if in.Status != StatusFailed || run.Status != RunFailed ||
		!strings.Contains(run.Error, "deploy app umami/production/web failed: container exited with status 1") {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	events := f.w.events()
	undone := events[len(events)-3:]
	if !slices.Equal(undone, []string{"delete app umami/production/web", "delete database umami-db", "delete project umami"}) {
		t.Errorf("undone = %v", undone)
	}
}

func TestAnInstallIntoAnExistingProjectLeavesThatProjectAlone(t *testing.T) {
	f := newFixture("0.7.0")
	f.w.projects["umami"] = []string{project.ProductionEnvSlug}
	f.w.deployFails = true
	in, _, _ := f.install(t, umamiRequest())

	if slices.ContainsFunc(in.Resources, func(r Resource) bool { return r.Kind == KindProject }) {
		t.Errorf("the existing project was recorded as created: %v", in.Resources)
	}
	for _, event := range f.w.events() {
		if strings.HasPrefix(event, "delete project") || strings.HasPrefix(event, "create project") ||
			strings.HasPrefix(event, "set image") {
			t.Errorf("the existing project was touched: %s", event)
		}
	}
}

func TestANewEnvironmentInAnExistingProjectIsCreatedAndUndone(t *testing.T) {
	f := newFixture("0.7.0")
	f.w.projects["umami"] = []string{project.ProductionEnvSlug}
	f.w.deployFails = true
	req := umamiRequest()
	req.Environment = "staging"
	f.install(t, req)

	events := f.w.events()
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
		"an answer that is not a hostname": {
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
			version := c.version
			if version == "" {
				version = "0.7.0"
			}
			f := newFixture(version)
			req := umamiRequest()
			c.prepare(f.w, &req)

			_, _, err := f.s.Install(context.Background(), admin, req)
			f.s.Wait()
			if err == nil || !c.check(err) {
				t.Fatalf("err = %v", err)
			}
			if len(f.records.installs) != 0 || len(f.w.events()) != 0 {
				t.Errorf("a refused install recorded %v and did %v", f.records.installs, f.w.events())
			}
		})
	}
}

func TestOnlyAnAdminChangesInstallations(t *testing.T) {
	f := newFixture("0.7.0")
	member := &user.User{ID: 2, Role: user.RoleMember}
	if _, _, err := f.s.Install(context.Background(), member, umamiRequest()); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("install: %v", err)
	}
	in, _, _ := f.install(t, umamiRequest())
	if _, err := f.s.PreviewUpdate(context.Background(), member, in.ID, ""); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("preview: %v", err)
	}
	if _, err := f.s.Uninstall(context.Background(), member, in.ID, true); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("uninstall: %v", err)
	}
}

func TestAPreviewSaysWhatAnUpdateWouldDoAndChangesNothing(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())

	if got, _ := f.s.Installs(context.Background(), admin); len(got) != 1 || got[0].Available != nil {
		t.Fatalf("before a newer release: %+v", got)
	}
	f.catalog.publish()
	if got, _ := f.s.Installs(context.Background(), admin); got[0].Available == nil || got[0].Available.Tag != "v1.3.0" {
		t.Errorf("the newer release was not offered: %+v", got[0].Available)
	}

	before := len(f.w.events())
	preview, err := f.s.PreviewUpdate(context.Background(), admin, in.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.w.events()) != before {
		t.Errorf("a preview did %v", f.w.events()[before:])
	}
	if preview.From != "v1.1.0" || preview.To != "v1.3.0" {
		t.Errorf("preview = %+v", preview)
	}
	for _, want := range []Change{
		{ActionChange, KindApp, "umami/production/web", "ghcr.io/umami-software/umami:3.3.1 → ghcr.io/umami-software/umami:3.4.0"},
		{ActionCreate, KindDatabase, "umami-cache", "redis 7.4"},
		{ActionChange, "variable", "SESSION_KEY", "set on umami/production/web"},
		{ActionKeep, "variable", "SELF", "no longer in the template; left on umami/production/web"},
	} {
		if !slices.Contains(preview.Changes, want) {
			t.Errorf("no %+v in %+v", want, preview.Changes)
		}
	}
	if len(preview.Inputs) != 1 || preview.Inputs[0].Key != "timezone" {
		t.Errorf("questions = %+v", preview.Inputs)
	}
}

func TestAnUpdateAppliesTheReleaseAndRecordsIt(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, secrets := f.install(t, umamiRequest())
	f.catalog.publish()

	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); err == nil {
		t.Fatal("an update with a question unanswered started")
	}
	run, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{Inputs: map[string]string{"timezone": "Europe/Lisbon"}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.Wait()

	done := f.records.run(run.ID)
	updated, _ := f.records.Install(context.Background(), in.ID)
	if done.Status != RunSucceeded || updated.Release != "v1.3.0" || updated.Answers["timezone"] != "Europe/Lisbon" {
		t.Fatalf("run = %+v, installation = %+v", done, updated)
	}
	web := f.w.app("umami/production/web")
	if web.SourceTag != "3.4.0" || web.Env["SESSION_KEY"] != secrets["appSecret"] || web.Env["TZ"] != "Europe/Lisbon" ||
		web.Env["SELF"] == "" {
		t.Errorf("web = %s %v", web.SourceTag, web.Env)
	}
	if !slices.Contains(updated.Resources, Resource{KindDatabase, "cache", "umami-cache"}) {
		t.Errorf("resources = %v", updated.Resources)
	}
	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); !errors.Is(err, ErrUpToDate) {
		t.Errorf("a second update: %v", err)
	}
}

func TestAFailedUpdatePutsTheInstallationBack(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, secrets := f.install(t, umamiRequest())
	f.catalog.publish()
	f.w.mu.Lock()
	f.w.deployFails = true
	f.w.mu.Unlock()

	run, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{Inputs: map[string]string{"timezone": "UTC"}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.Wait()

	done := f.records.run(run.ID)
	after, _ := f.records.Install(context.Background(), in.ID)
	if done.Status != RunFailed || after.Release != "v1.1.0" || after.Status != StatusInstalled {
		t.Fatalf("run = %+v, installation = %+v", done, after)
	}
	web := f.w.app("umami/production/web")
	if web.SourceTag != "3.3.1" || web.Env["APP_SECRET"] != secrets["appSecret"] {
		t.Errorf("web was not put back: %s %v", web.SourceTag, web.Env)
	}
	if _, set := web.Env["SESSION_KEY"]; set {
		t.Errorf("a variable the update added is still set: %v", web.Env)
	}
	if !slices.Contains(f.w.events(), "delete database umami-cache") {
		t.Errorf("the database the update created was left: %v", f.w.events())
	}
}

func TestUninstallKeepsTheDataUnlessToldOtherwise(t *testing.T) {
	for _, keep := range []bool{true, false} {
		f := newFixture("0.7.0")
		in, _, _ := f.install(t, umamiRequest())

		run, err := f.s.Uninstall(context.Background(), admin, in.ID, keep)
		if err != nil {
			t.Fatal(err)
		}
		f.s.Wait()

		done := f.records.run(run.ID)
		after, _ := f.records.Install(context.Background(), in.ID)
		if done.Status != RunSucceeded || after.Status != StatusUninstalled {
			t.Fatalf("keep=%v: run = %+v, installation = %+v", keep, done, after)
		}
		events := f.w.events()
		if !slices.Contains(events, "delete app umami/production/web") || !slices.Contains(events, "delete project umami") {
			t.Errorf("keep=%v: %v", keep, events)
		}
		if slices.Contains(events, "delete database umami-db") == keep {
			t.Errorf("keep=%v, and the database was deleted: %v", keep, slices.Contains(events, "delete database umami-db"))
		}
		if _, err := f.s.Uninstall(context.Background(), admin, in.ID, keep); !errors.Is(err, ErrNotInstalled) {
			t.Errorf("uninstalling twice: %v", err)
		}
	}
}

func TestUninstallLeavesAProjectThatStillHasApps(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	f.w.mu.Lock()
	f.w.apps["umami/production/mine"] = &app.Scoped{ProjectSlug: "umami", EnvironmentSlug: "production"}
	f.w.mu.Unlock()

	if _, err := f.s.Uninstall(context.Background(), admin, in.ID, true); err != nil {
		t.Fatal(err)
	}
	f.s.Wait()
	if slices.Contains(f.w.events(), "delete project umami") {
		t.Error("a project with somebody's app in it was deleted")
	}
}

func TestRecoverUndoesAnInstallAPreviousDaemonLeftRunning(t *testing.T) {
	f := newFixture("0.7.0")
	f.records.installs[1] = Install{ID: 1, Status: StatusInstalling}
	f.records.runs[1] = Run{ID: 1, InstallID: 1, Kind: RunInstall, Status: RunRunning, CreatedBy: admin.ID,
		Created: []Resource{{KindProject, "", "umami"}, {KindDatabase, "db", "umami-db"}}}
	f.records.installs[2] = Install{ID: 2, Status: StatusInstalling}
	f.records.runs[2] = Run{ID: 2, InstallID: 2, Kind: RunInstall, Status: RunRunning,
		Created: []Resource{{KindDatabase, "db", "orphan-db"}}}

	if err := f.s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.w.events(), []string{"delete database umami-db", "delete project umami"}) {
		t.Errorf("events = %v", f.w.events())
	}
	for id := range f.records.runs {
		if run := f.records.run(id); run.Status != RunFailed || run.Error == "" {
			t.Errorf("run %d = %+v", id, run)
		}
	}
	if f.records.installs[1].Status != StatusFailed || f.records.installs[2].Status != StatusFailed {
		t.Errorf("installations = %+v", f.records.installs)
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
