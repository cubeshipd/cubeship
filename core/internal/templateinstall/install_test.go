package templateinstall

import (
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

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
	// hostPorts are the host ports something on the instance publishes.
	hostPorts map[int]bool

	specs       []datastore.Spec
	deployFails bool
	nextDomain  int64
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

func (f fakeApps) ConfigureNetworking(_ context.Context, _ *user.User, ref app.Reference, ports []int, checks []app.HealthCheck) (*app.Scoped, error) {
	a := f.apps[ref.String()]
	a.InternalPorts = append([]int(nil), ports...)
	a.HealthChecks = append([]app.HealthCheck(nil), checks...)
	return a, nil
}

func (f fakeApps) AddDomain(_ context.Context, _ *user.User, ref app.Reference, host string, port int) (*app.Scoped, error) {
	f.mu.Lock()
	// One app per name, as the unique index has it.
	for _, other := range f.apps {
		if slices.ContainsFunc(other.Domains, func(d app.Domain) bool { return d.Host == host }) {
			f.mu.Unlock()
			return nil, app.ErrDomainTaken
		}
	}
	a := f.apps[ref.String()]
	f.nextDomain++
	a.Domains = append(a.Domains, app.Domain{ID: f.nextDomain, Host: host, Port: port})
	f.mu.Unlock()
	f.did("add domain " + host + " to " + ref.String())
	return &app.Scoped{}, nil
}

func (f fakeApps) RemoveDomain(_ context.Context, _ *user.User, ref app.Reference, id int64) (*app.Scoped, error) {
	f.mu.Lock()
	if a, ok := f.apps[ref.String()]; ok {
		a.Domains = slices.DeleteFunc(slices.Clone(a.Domains), func(d app.Domain) bool { return d.ID == id })
	}
	f.mu.Unlock()
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

func (f fakeApps) AddTCPPort(_ context.Context, _ *user.User, ref app.Reference, containerPort, hostPort int) (*app.TCPPort, error) {
	f.mu.Lock()
	a := f.apps[ref.String()]
	p := app.TCPPort{ID: int64(len(a.TCPPorts) + 1), ContainerPort: containerPort, HostPort: hostPort}
	a.TCPPorts = append(a.TCPPorts, p)
	f.mu.Unlock()
	f.did(fmt.Sprintf("publish port %d of %s on %d", containerPort, ref, hostPort))
	return &p, nil
}

func (f fakeApps) RemoveTCPPort(_ context.Context, _ *user.User, ref app.Reference, id int64) error {
	f.did(fmt.Sprintf("stop publishing port %d of %s", id, ref))
	return nil
}

func (f fakeApps) TCPPortTaken(_ context.Context, hostPort int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hostPorts[hostPort], nil
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
