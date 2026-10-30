package templateinstall

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/project"
	"cubeship/internal/user"
)

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

func (r *fakeRecords) SetInstallationName(_ context.Context, id int64, name string) (*Install, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in, ok := r.installs[id]
	if !ok || in.Status != StatusInstalled || in.InstallationName != "" {
		return nil, ErrNotFound
	}
	in.InstallationName = name
	r.installs[id] = in
	return &in, nil
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

func TestTemplateInstallationNameUsesTheTemplateTitleSlug(t *testing.T) {
	for _, tc := range []struct{ title, repo, want string }{
		{"Jian", "jian", "jian"},
		{"Cal.com", "cal-com", "cal-com"},
		{"Settings", "settings-template", "settings-template"},
		{"!!!", "template", "template"},
	} {
		if got := templateSlug(tc.title, tc.repo); got != tc.want {
			t.Errorf("templateSlug(%q, %q) = %q, want %q", tc.title, tc.repo, got, tc.want)
		}
	}
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
	want := []Resource{{KindProject, "", "umami"}, {KindDatabase, "db", "cubeship-umami-template-umami-db"}, {KindApp, "web", "umami/production/cubeship-umami-template-web"}}
	if !slices.Equal(in.Resources, want) || !slices.Equal(run.Created, want) {
		t.Errorf("resources = %v, created = %v, want %v", in.Resources, run.Created, want)
	}
	if in.Manifest == nil || in.Answers["domain"] != "analytics.example.com" || in.Answers["appSecret"] != "" {
		t.Errorf("recorded manifest %v and answers %v", in.Manifest != nil, in.Answers)
	}
	if len(secrets["appSecret"]) != 32 {
		t.Fatalf("generated secret = %q", secrets["appSecret"])
	}
	if in.InstallationName != "cubeship-umami-template" {
		t.Fatalf("default installation name = %q, want cubeship-umami-template", in.InstallationName)
	}
	web := f.w.app("umami/production/cubeship-umami-template-web")
	if web.Env["APP_SECRET"] != secrets["appSecret"] || web.Env["DB_HOST"] != "cubeship-db-cubeship-umami-template-umami-db" ||
		web.Env["SELF"] != "http://cubeship-umami-production-cubeship-umami-template-web:3000" {
		t.Errorf("variables = %v", web.Env)
	}
	if web.Limits.CPU != 1 || web.Limits.Memory != 1<<30 {
		t.Errorf("limits = %+v", web.Limits)
	}
	if spec := f.w.specs[0]; spec.Engine != datastore.EnginePostgres || spec.Version != "18" || spec.Database != "umami" {
		t.Errorf("database spec = %+v", spec)
	}
	for _, event := range []string{
		"create app umami/production/cubeship-umami-template-web external ghcr.io/umami-software/umami:3.3.1",
		"add domain analytics.example.com to umami/production/cubeship-umami-template-web",
		"attach cubeship-umami-template-umami-db to umami/production/cubeship-umami-template-web",
		"deploy umami/production/cubeship-umami-template-web",
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
		!strings.Contains(run.Error, "deploy app umami/production/cubeship-umami-template-web failed: container exited with status 1") {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	events := f.w.events()
	undone := events[len(events)-3:]
	if !slices.Equal(undone, []string{"delete app umami/production/cubeship-umami-template-web", "delete database cubeship-umami-template-umami-db", "delete project umami"}) {
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
			prepare: func(w *world, _ *Request) { w.dbs["cubeship-umami-template-umami-db"] = true },
			check: func(err error) bool {
				var taken *TakenError
				return errors.As(err, &taken) && taken.Name == "cubeship-umami-template-umami-db"
			},
		},
		"an answer that is not a hostname": {
			prepare: func(w *world, req *Request) {
				w.dbs["cubeship-umami-template-umami-db"] = true
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
	if _, err := f.s.Uninstall(context.Background(), member, in.ID, true); !errors.Is(err, user.ErrForbidden) {
		t.Errorf("uninstall: %v", err)
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
		if !slices.Contains(events, "delete app umami/production/cubeship-umami-template-web") || !slices.Contains(events, "delete project umami") {
			t.Errorf("keep=%v: %v", keep, events)
		}
		if slices.Contains(events, "delete database cubeship-umami-template-umami-db") == keep {
			t.Errorf("keep=%v, and the database was deleted: %v", keep, slices.Contains(events, "delete database cubeship-umami-template-umami-db"))
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
		Created: []Resource{{KindProject, "", "umami"}, {KindDatabase, "db", "cubeship-umami-template-umami-db"}}}
	f.records.installs[2] = Install{ID: 2, Status: StatusInstalling}
	f.records.runs[2] = Run{ID: 2, InstallID: 2, Kind: RunInstall, Status: RunRunning,
		Created: []Resource{{KindDatabase, "db", "orphan-db"}}}

	if err := f.s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.w.events(), []string{"delete database cubeship-umami-template-umami-db", "delete project umami"}) {
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

// New updates are gone, but upgrades must still recover an old daemon's pending update.
func TestRecoverRollsBackAHistoricalUpdate(t *testing.T) {
	f := newFixture("0.7.0")
	in, _, _ := f.install(t, umamiRequest())
	run, err := f.records.CreateRun(context.Background(), &Run{InstallID: in.ID, Kind: RunUpdate, Status: RunRunning, CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := f.records.run(run.ID)
	if got.Status != RunFailed || !strings.Contains(got.Error, "daemon restarted") {
		t.Fatalf("historical update: %+v", got)
	}
	if f.records.installs[in.ID].Status != StatusInstalled {
		t.Fatal("recovery lost the installation")
	}
}
