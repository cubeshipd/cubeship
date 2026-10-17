package app

import (
	"context"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/project"
	"cubeship/internal/settings"
	"cubeship/internal/user"
	"errors"
	"log"
	"time"
)

// Service holds the app use cases. Authorization is the caller's own
// role: see user.Require, and RoleToDeploy for the one place the answer
// depends on what the app is.
type Service struct {
	db       *database.DB
	projects *project.Service
	orch     *Orchestrator
	settings *settings.Service
	// metrics answers what this app's container has been using. Held
	// rather than reached for, because the endpoint that serves it is
	// this module's — an app's series lives at the app's address, where
	// this module has already decided who may look.
	metrics *metrics.Service

	// datastores is what an attached database contributes to an app's
	// environment. See DatastoreVars: the module that implements it
	// sits above this one, so the daemon hands it back down.
	datastores DatastoreVars
	// objectStores is the same for the buckets attached to it.
	objectStores ObjectStoreVars

	// remote reaches a machine an app is placed on, for the things only
	// that machine has. Nil on a daemon with no cluster module wired
	// in, and then an app is only ever here.
	remote Remote

	// routesChanged is told when the set of live containers moves, so
	// the file this instance's proxy reads is rewritten now rather than
	// on the next tick. Nil on a server with nobody listening, which is
	// a test.
	routesChanged func()

	// dataDir is where volumes on this machine keep their data. Empty on
	// a server with no disk of its own, which is a test.
	dataDir string

	// portsChanged is told after anything changes which host ports apps
	// publish, and hostPorts answers which ones everything on this
	// instance already does. Both nil on a server with no firewall wired
	// in, which is a test.
	portsChanged PortsChanged
	hostPorts    HostPorts
}

// PortsChanged is told whenever what this module publishes on the host
// changes. Satisfied by the firewall, which sits above this module.
type PortsChanged interface {
	SyncPublished(ctx context.Context) error
}

// HostPorts is every host port this instance deliberately publishes —
// datastores', stores' and apps' — so a port named for an app is refused
// while a database holds it, rather than failing at bind time on a deploy.
type HostPorts interface {
	ExposedPorts(ctx context.Context) ([]int, error)
}

// SetPortsChanged names what is told when a published port comes or goes.
// Called once, by server.New.
func (s *Service) SetPortsChanged(p PortsChanged) { s.portsChanged = p }

// SetHostPorts names what answers which host ports are taken. Called once,
// by server.New.
func (s *Service) SetHostPorts(h HostPorts) { s.hostPorts = h }

// syncPorts tells the firewall, best effort: the port is published (or
// not) by the next deploy either way, and a firewall that could not be
// rewritten is not a reason to report that as a failure.
func (s *Service) syncPorts(ctx context.Context) {
	if s.portsChanged == nil {
		return
	}
	if err := s.portsChanged.SyncPublished(ctx); err != nil {
		log.Printf("app: could not bring the host firewall in line with what is published: %v", err)
	}
}

// SetDataDir tells the module where this machine's volumes live. Called
// once, by server.New.
func (s *Service) SetDataDir(dir string) {
	s.dataDir = dir
	s.orch.dataDir = dir
}

// Remote is how this module reaches the machine an app runs on.
//
// Declared here and satisfied by `node`, which owns the conversation
// with a machine — the same direction DatastoreVars and AppTeardown
// run. What it is for is the things only that machine has: its
// containers' logs today, and whatever else the channel carries later.
type Remote interface {
	Logs(ctx context.Context, nodeID int64, containerID, tail string) ([]byte, error)
	Wake(nodeID int64)
	// Addresses is where each machine in the cluster is reached, by
	// node id — what a DNS record for an app on it has to point at.
	Addresses(ctx context.Context) (map[int64]string, error)
	// Quiet is the machines that have not called in for at least d, by
	// node id. What a stalled deploy is worked out from: a machine that
	// is merely slow is still calling in, and how long the silence has
	// to have lasted is this module's judgement rather than the
	// cluster's.
	Quiet(ctx context.Context, d time.Duration) (map[int64]bool, error)
}

// SetRoutesChanged wires in what to tell when the set of live
// containers moves — a deploy that swapped one, a machine reporting a
// new one, a placement that changed.
//
// Without it the routes file is only as fresh as its ticker, and a
// deploy leaves it naming a container that has just been removed: a 502
// on every request for that name until the next pass. Called once, by
// cmd/cubeshipd, with the writer's own Wake.
func (s *Service) SetRoutesChanged(fn func()) {
	s.routesChanged = fn
	s.orch.routesChanged = fn
}

// routesChanged says the answer moved, when there is anybody to tell.
func (s *Service) announceRoutes() {
	if s.routesChanged != nil {
		s.routesChanged()
	}
}

// SetRemote wires it in. Called once, by server.New — and the
// orchestrator gets it too, for the one thing it does with it: waking
// the machine a deploy was just placed on.
func (s *Service) SetRemote(r Remote) {
	s.remote = r
	s.orch.remote = r
}

func NewService(db *database.DB, projects *project.Service, orch *Orchestrator,
	cfg *settings.Service, series *metrics.Service) *Service {
	return &Service{db: db, projects: projects, orch: orch, settings: cfg, metrics: series}
}

// Metrics exposes the series service to this module's own handlers.
func (s *Service) Metrics() *metrics.Service { return s.metrics }

// MetricSubjects is what the collector samples on this module's behalf:
// every app with a container behind it right now. See metrics.Source —
// nothing about an app travels into that package but its id.
func (s *Service) MetricSubjects(ctx context.Context) ([]metrics.Subject, error) {
	// The scoped list, because the name that travels has to be the
	// reference: `gateway` is unique inside one environment and nowhere
	// else, and a list of what is using this machine that says
	// `gateway` names something the reader cannot find.
	all, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	// This machine's own replicas, and no others: the collector reads a
	// cgroup through this Engine, and a container on another machine is
	// one it would ask about and be told does not exist. Those machines
	// take their own readings and report them — see Sampled.
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]metrics.Subject, 0, len(all))
	for _, a := range all {
		mine, ok := a.ReplicaOn(here)
		if !ok || mine.Container == "" {
			continue
		}
		out = append(out, metrics.Subject{
			Kind: metrics.KindApp, ID: a.ID, ContainerID: mine.Container,
			Name: Reference{Project: a.ProjectSlug, Environment: a.EnvironmentSlug, Name: a.Name}.String(),
		})
	}
	return out, nil
}

// Series is an app's recent CPU and memory.
//
// Resolved at member first, like every other read of an app: what it is
// using is part of knowing whether it is healthy, which is not an
// admin's question.
func (s *Service) Series(ctx context.Context, caller *user.User, ref Reference, window string) (metrics.Series, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return metrics.Series{}, err
	}
	// An app on several machines has several containers writing into
	// one series, so a bucket holds a reading from each. The chart is
	// therefore **the average across its replicas**, and its peak is
	// the busiest replica's — which is what somebody looking at "is
	// this app struggling" wants from either shape.
	return s.metrics.Series(ctx, metrics.KindApp, a.ID, window, a.HasContainer())
}

// SetDatastoreVars wires the datastore module in. Called once, at
// startup, by the only package that knows every module exists.
//
// Both this service and the orchestrator need it — one to say what an
// app's environment resolves to, the other to build the container's —
// and they must not be able to answer differently, so there is one
// setter for both.
func (s *Service) SetDatastoreVars(v DatastoreVars) {
	s.datastores = v
	s.orch.datastores = v
}

// SetObjectStoreVars wires the object storage module in, for the same
// reason and in the same place.
func (s *Service) SetObjectStoreVars(v ObjectStoreVars) {
	s.objectStores = v
	s.orch.objectStores = v
}

// registryHost is where apps are pushed, or "" while the instance has no
// domain. Read per call rather than captured at startup, because an
// operator configures the domain from the dashboard after installing —
// the answer changes without a restart.
func (s *Service) RegistryHost(ctx context.Context) string {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return settings.RegistryHostFor(values.Get(settings.Domain))
}

// Instance is the configuration an app's response depends on that the
// app itself does not carry. It is read once per request rather than
// once per app in a listing.
type Instance struct {
	// RegistryHost is where a push goes, or "" while there is no domain
	// to derive one from.
	RegistryHost string
	// Domain is the instance's own, which is what a suggested host for
	// an app is built under.
	Domain string
	// PublicIP is where this machine is reached, and Addresses is the
	// same for every other machine in the cluster, by node id.
	//
	// Both, because an app's traffic arrives at the machine the app is
	// on: each node runs its own edge, so a name pointing at the
	// control plane reaches nothing when the app is somewhere else.
	// Read once per request rather than per app — a listing would
	// otherwise be one query per row.
	PublicIP  string
	Addresses map[int64]string
}

// InstanceConfig reads what a response needs about the instance.
func (s *Service) InstanceConfig(ctx context.Context) Instance {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return Instance{}
	}
	domain := values.Get(settings.Domain)
	in := Instance{
		RegistryHost: settings.RegistryHostFor(domain),
		Domain:       domain,
		PublicIP:     s.settings.PublicIP(ctx, values, ""),
	}
	if s.remote != nil {
		// Empty on a daemon with no cluster, which is every instance of
		// one machine: everything falls back to this box's own address.
		in.Addresses, _ = s.remote.Addresses(ctx)
	}
	return in
}

// ImageFor returns the registry path a push to this app targets, or ""
// while no domain is configured — there is nowhere to push to yet.
func (s *Service) ImageFor(ctx context.Context, a *Scoped) string {
	host := s.RegistryHost(ctx)
	if host == "" {
		return ""
	}
	return ReferenceOf(a).ImageFor(host)
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Orchestrator exposes the deploy engine for the registry webhook, which
// deploys without a caller to authorize.
func (s *Service) Orchestrator() *Orchestrator { return s.orch }

// Resolve looks up an app by reference and requires minRole of the
// caller.
func (s *Service) Resolve(ctx context.Context, caller *user.User, ref Reference, need user.Level) (*Scoped, error) {
	if err := allowApps(caller, ref.Project, need); err != nil {
		return nil, err
	}
	a, err := s.Repo().ScopedByReference(ctx, ref.Project, ref.Environment, ref.Name)
	if err != nil {
		return nil, ErrNotFound
	}
	// Loaded here rather than joined, so every caller that resolves an
	// app has its domains without any of them remembering to ask.
	domains, err := s.Repo().Domains(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	a.Domains = domains
	return a, nil
}

// ResolveString is Resolve for a reference that still has to be parsed.
func (s *Service) ResolveString(ctx context.Context, caller *user.User, ref string, need user.Level) (*Scoped, error) {
	parsed, err := ParseReference(ref)
	if err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, parsed, need)
}

// List returns every app on the instance.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Scoped, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	apps, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	if !caller.Admin() {
		seen := apps[:0]
		for _, a := range apps {
			if user.Sees(caller, user.ResApps, a.ProjectSlug) {
				seen = append(seen, a)
			}
		}
		apps = seen
	}
	return s.withDomains(ctx, apps)
}

// withDomains fills in what each app answers at, in one query rather
// than one per app.
func (s *Service) withDomains(ctx context.Context, apps []*Scoped) ([]*Scoped, error) {
	ids := make([]int64, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	byApp, err := s.Repo().DomainsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		a.Domains = byApp[a.ID]
	}
	return apps, nil
}

// allowApps is Allow on the apps in a project, answering a project
// outside the caller's grant as the app not being there.
func allowApps(caller *user.User, projectSlug string, need user.Level) error {
	err := user.Allow(caller, user.ResApps, need, projectSlug)
	if errors.Is(err, user.ErrHidden) {
		return ErrNotFound
	}
	return err
}
