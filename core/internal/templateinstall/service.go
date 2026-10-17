package templateinstall

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"slices"
	"sync"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/template"
	"cubeship/internal/user"
)

// What an install creates things through. Each is the module's own
// service; they are interfaces so the order and the undoing can be tested
// without a database or a Docker behind them.

type Projects interface {
	Resolve(ctx context.Context, caller *user.User, projectSlug string, need user.Level) (*project.Project, error)
	ResolveEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string, need user.Level) (*project.Environment, error)
	Create(ctx context.Context, caller *user.User, projectSlug string) (*project.Project, *project.Environment, error)
	CreateEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*project.Environment, error)
	Delete(ctx context.Context, caller *user.User, projectSlug string) (*project.Project, error)
	DeleteEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*project.Environment, error)
	SetImage(ctx context.Context, caller *user.User, projectSlug string, body io.Reader) error
}

type Apps interface {
	Resolve(ctx context.Context, caller *user.User, ref app.Reference, need user.Level) (*app.Scoped, error)
	Create(ctx context.Context, caller *user.User, projectSlug, envSlug, name string, source app.Source, origin app.Origin) (*app.Scoped, error)
	Update(ctx context.Context, caller *user.User, ref app.Reference, source *app.Source, origin *app.Origin, health *string, limits *app.Limits, auto *app.Autoscale, place *app.Placement) (*app.Scoped, error)
	AddDomain(ctx context.Context, caller *user.User, ref app.Reference, host string, port int) (*app.Scoped, error)
	MergeEnv(ctx context.Context, caller *user.User, ref app.Reference, set envvar.Map, unset []string) (*app.Scoped, error)
	Deploy(ctx context.Context, caller *user.User, ref app.Reference, tag string) (*app.Scoped, *app.Deployment, error)
	WaitForDeployment(ctx context.Context, caller *user.User, ref app.Reference, deploymentID int64) (*app.Deployment, error)
	DeleteApp(ctx context.Context, caller *user.User, ref app.Reference, deleteVolumeData bool) (*app.Scoped, error)
	AddVolume(ctx context.Context, caller *user.User, ref app.Reference, containerPath string) (*app.Volume, error)
	RemoveVolume(ctx context.Context, caller *user.User, ref app.Reference, volumeID int64, deleteData bool) error
	AddTCPPort(ctx context.Context, caller *user.User, ref app.Reference, containerPort, hostPort int) (*app.TCPPort, error)
	RemoveTCPPort(ctx context.Context, caller *user.User, ref app.Reference, portID int64) error
	TCPPortTaken(ctx context.Context, hostPort int) (bool, error)
	HostTaken(ctx context.Context, host string) (bool, error)
	Env(ctx context.Context, caller *user.User, ref app.Reference) (envvar.Map, []envvar.Resolved, error)
	List(ctx context.Context, caller *user.User) ([]*app.Scoped, error)
	RemoveDomain(ctx context.Context, caller *user.User, ref app.Reference, domainID int64) (*app.Scoped, error)
}

type Datastores interface {
	Resolve(ctx context.Context, caller *user.User, name string, need user.Level) (*datastore.Datastore, error)
	Create(ctx context.Context, caller *user.User, spec datastore.Spec) (*datastore.Datastore, error)
	Credentials(ctx context.Context, caller *user.User, name string) (datastore.Credentials, error)
	Attach(ctx context.Context, caller *user.User, name, appRef, prefix string) (*datastore.Datastore, error)
	Detach(ctx context.Context, caller *user.User, name, appRef string) (*datastore.Datastore, error)
	Delete(ctx context.Context, caller *user.User, name string) (*datastore.Datastore, error)
}

type ObjectStores interface {
	Resolve(ctx context.Context, caller *user.User, name string, need user.Level) (*objectstore.Store, error)
	Create(ctx context.Context, caller *user.User, spec objectstore.ManagedSpec) (*objectstore.Store, error)
	Credentials(ctx context.Context, caller *user.User, name string) (objectstore.Credentials, error)
	CreateBucket(ctx context.Context, caller *user.User, name, bucket string) error
	Attach(ctx context.Context, caller *user.User, name, appRef, bucket, prefix string) (*objectstore.Store, error)
	Detach(ctx context.Context, caller *user.User, name, appRef, bucket string) (*objectstore.Store, error)
	Delete(ctx context.Context, caller *user.User, name string) (*objectstore.Store, error)
}

// Users finds who started an install, to undo it as them after a restart.
type Users interface {
	ByID(ctx context.Context, id int64) (*user.User, error)
}

// Records is where installations and their runs are kept. *Repository
// is one.
type Records interface {
	CreateInstall(ctx context.Context, in *Install) (*Install, error)
	SaveInstall(ctx context.Context, in *Install) error
	Install(ctx context.Context, id int64) (*Install, error)
	Installs(ctx context.Context) ([]*Install, error)
	CreateRun(ctx context.Context, run *Run) (*Run, error)
	SaveRun(ctx context.Context, run *Run) error
	Runs(ctx context.Context, installID int64) ([]*Run, error)
	RunningRuns(ctx context.Context) ([]*Run, error)
}

// RoleToInstall is admin: an install creates databases, and a template's
// app may build a repository on this host.
const RoleToInstall = user.LevelManage

type Service struct {
	records    Records
	projects   Projects
	apps       Apps
	datastores Datastores
	stores     ObjectStores
	users      Users
	version    string

	mu      sync.RWMutex
	catalog Catalog

	wg sync.WaitGroup

	// Poll is how often a wait for a database or a store looks again.
	Poll time.Duration
	// Timeout is the longest an install may run before it is undone, for
	// a template whose apps build nothing. See timeoutFor.
	Timeout time.Duration
}

func NewService(records Records, projects Projects, apps Apps, datastores Datastores, stores ObjectStores, users Users, catalog Catalog, version string) *Service {
	return &Service{
		records: records, projects: projects, apps: apps, datastores: datastores, stores: stores, users: users,
		catalog: catalog, version: version,
		Poll: 2 * time.Second, Timeout: 30 * time.Minute,
	}
}

// timeoutFor is how long an install or update of m may run: Timeout, and
// a build's whole budget again for every app that builds, because apps
// deploy one after another and each build may take all of its own.
func (s *Service) timeoutFor(m *template.Normalized) time.Duration {
	d := s.Timeout
	if m == nil {
		return d
	}
	for _, a := range m.Apps {
		if a.Source.Type != "image" {
			d += app.BuildTimeout
		}
	}
	return d
}

// SetCatalog replaces where templates come from. For tests.
func (s *Service) SetCatalog(c Catalog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalog = c
}

func (s *Service) source() (Catalog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return nil, ErrCatalogUnavailable
	}
	return s.catalog, nil
}

// Wait blocks until every run this process started has finished. For
// tests, and for a daemon shutting down.
func (s *Service) Wait() { s.wg.Wait() }

func (s *Service) start(run func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		run()
	}()
}

// Templates is a page of the catalog.
func (s *Service) Templates(ctx context.Context, caller *user.User, query url.Values) (json.RawMessage, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.List(ctx, query)
}

// Tags is every tag a listed template carries, for the filter.
func (s *Service) Tags(ctx context.Context, caller *user.User) (json.RawMessage, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.Tags(ctx)
}

// Template is one template, with its README, file and manifest.
func (s *Service) Template(ctx context.Context, caller *user.User, owner, repo string) (json.RawMessage, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.Template(ctx, owner, repo)
}

// Icon is a release's icon, fetched through the instance so the
// dashboard's browser never talks to the catalog.
func (s *Service) Icon(ctx context.Context, caller *user.User, repository, file string) ([]byte, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.Icon(ctx, repository, file)
}

// Installed is an installation with what a screen shows about it.
type Installed struct {
	Install *Install
	// Runs is its most recent runs, newest first.
	Runs []*Run
}

// Busy reports whether a run is changing the installation now.
func (i Installed) Busy() bool { return len(i.Runs) > 0 && i.Runs[0].Status == RunRunning }

// Installs is what is installed, or being installed, newest first.
func (s *Service) Installs(ctx context.Context, caller *user.User) ([]Installed, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	all, err := s.records.Installs(ctx)
	if err != nil {
		return nil, err
	}
	out := []Installed{}
	for _, in := range all {
		if in.Status != StatusInstalled && in.Status != StatusInstalling {
			continue
		}
		runs, err := s.records.Runs(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		if len(runs) > 1 {
			runs = runs[:1]
		}
		out = append(out, Installed{Install: in, Runs: runs})
	}
	return out, nil
}

// Get is one installation, its runs, and whether a newer release exists.
func (s *Service) Get(ctx context.Context, caller *user.User, id int64) (*Installed, error) {
	if err := user.Allow(caller, user.ResTemplates, user.LevelView, ""); err != nil {
		return nil, err
	}
	in, err := s.records.Install(ctx, id)
	if err != nil {
		return nil, err
	}
	runs, err := s.records.Runs(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Installed{Install: in, Runs: runs}, nil
}

func (s *Service) busy(ctx context.Context, installID int64) (bool, error) {
	runs, err := s.records.Runs(ctx, installID)
	return len(runs) > 0 && runs[0].Status == RunRunning, err
}

// Request is what an install is asked to do.
type Request struct {
	Owner string
	Repo  string
	// Release is a tag the catalog accepted. Empty is the newest one.
	Release string
	// Project and Environment are where the apps go. Empty takes the
	// template's suggestion; either is created when it does not exist.
	Project     string
	Environment string
	// Databases, Stores and Apps rename what the template declares, by
	// key. A key left out keeps the template's name.
	Databases map[string]string
	Stores    map[string]string
	Apps      map[string]string
	// Inputs answers the template's questions, by key.
	Inputs map[string]string
}

// Install checks the request against the template and the instance,
// records the install and starts it. Nothing is created before every
// check has passed.
//
// The secrets the instance generated come back here and nowhere else:
// they are not recorded, only written into the apps that use them.
func (s *Service) Install(ctx context.Context, caller *user.User, req Request) (*Installed, map[string]string, error) {
	if err := user.Allow(caller, user.ResTemplates, RoleToInstall, ""); err != nil {
		return nil, nil, err
	}
	p, err := s.prepare(ctx, caller, req)
	if err != nil {
		return nil, nil, err
	}
	in, err := s.records.CreateInstall(ctx, &Install{
		Owner: p.owner, Repo: p.repo, Release: p.release.Tag, Commit: p.release.Commit,
		Project: p.project, Environment: p.environment, Status: StatusInstalling,
		Manifest: p.manifest, Answers: p.answers(), CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, nil, err
	}
	run, err := s.records.CreateRun(ctx, &Run{
		InstallID: in.ID, Kind: RunInstall, ToRelease: p.release.Tag,
		Status: RunRunning, Step: "Starting", CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, nil, err
	}
	started := &Installed{Install: copyInstall(in), Runs: []*Run{copyRun(run)}}
	s.start(func() { s.runInstall(caller, in, run, p) })
	return started, p.generated, nil
}

func copyInstall(in *Install) *Install {
	c := *in
	c.Resources = slices.Clone(in.Resources)
	return &c
}

func copyRun(r *Run) *Run {
	c := *r
	c.Created = slices.Clone(r.Created)
	c.Snapshot = slices.Clone(r.Snapshot)
	return &c
}

// plan is a request checked against the template and the instance, with
// every name and answer settled.
type plan struct {
	owner, repo string
	release     CatalogRelease
	manifest    *template.Normalized

	project, environment string
	createProject        bool
	createEnvironment    bool

	// databases, stores and apps are each template key's name on the
	// instance.
	databases map[string]string
	stores    map[string]string
	apps      map[string]string
	// storeInputs is each store input's answer: a store that already exists.
	storeInputs map[string]string
	inputs      map[string]string
	generated   map[string]string
}

// ReleaseOption is a release a template can be installed at.
type ReleaseOption struct {
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	PublishedAt string `json:"published_at,omitempty"`
}

// ReleaseManifest is one release as this instance read and checked it.
type ReleaseManifest struct {
	Release  ReleaseOption        `json:"release"`
	Manifest *template.Normalized `json:"manifest"`
	// Fits is false for a release that needs a newer Cubeship than this
	// instance runs, and Problem says so. The install would refuse it.
	Fits    bool   `json:"fits"`
	Problem string `json:"problem,omitempty"`
}
