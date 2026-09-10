package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/metrics"
	"cubeship/internal/node"
	"cubeship/internal/platform/database"
	"cubeship/internal/project"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
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
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
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
func (s *Service) Resolve(ctx context.Context, caller *user.User, ref Reference, minRole user.Role) (*Scoped, error) {
	if err := user.Require(caller, minRole); err != nil {
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
func (s *Service) ResolveString(ctx context.Context, caller *user.User, ref string, minRole user.Role) (*Scoped, error) {
	parsed, err := ParseReference(ref)
	if err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, parsed, minRole)
}

// Create registers an app in a project's environment and returns it,
// including the registry path a push should target.
func (s *Service) Create(ctx context.Context, caller *user.User, projectSlug, envSlug, name, description string, source Source, origin Origin) (*Scoped, error) {
	if envSlug == "" {
		envSlug = project.ProductionEnvSlug
	}
	if source == "" {
		source = DefaultSource
	}
	if !source.Valid() {
		return nil, ErrUnknownSource
	}
	if err := checkOrigin(source, &origin); err != nil {
		return nil, err
	}
	// The name becomes the last path component of the app's registry
	// image reference, so it is checked before anything is looked up.
	if slug.Reserved(name) {
		return nil, slug.ErrReserved
	}
	if !slug.Valid(name) {
		return nil, slug.ErrInvalid
	}

	// Creating an app that builds is deciding that this instance will
	// execute whatever that source contains, so it takes the same role
	// deploying it does. A member creating one they could never deploy
	// would be an odd thing to allow.
	if err := user.Require(caller, RoleToDeploy(source)); err != nil {
		return nil, err
	}
	p, err := s.projects.Repo().BySlug(ctx, projectSlug)
	if err != nil {
		return nil, project.ErrNotFound
	}
	env, err := s.projects.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, project.ErrEnvironmentNotFound
	}
	ref := Reference{Project: p.Slug, Environment: env.Slug, Name: name}
	if _, err := s.Repo().Create(ctx, p.ID, env.ID, name, description, source, origin); err != nil {
		// The unique index is the authority, not a preceding lookup:
		// two concurrent creates of the same name would both pass a
		// check and the loser would surface as a 500.
		if database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return s.Repo().ScopedByReference(ctx, ref.Project, ref.Environment, ref.Name)
}

// Delete removes an app: its container first, then its rows. The order
// matters — a row deleted while the container runs leaves something
// serving traffic that nothing knows how to stop.
//
// Images already pushed stay in the registry. Reclaiming them needs a
// registry garbage collection pass, which is a separate operation.
// Update reconfigures an app: its description, where its image comes
// from, how Traefik decides it is healthy, and which machines run it.
//
// An app is created with almost none of that, so this is where it
// becomes deployable. Changing the source to one that builds is the same
// decision as creating one that builds — this instance will execute
// whatever that repository contains — so it takes the same role, checked
// against the source being moved to rather than the one being left.
func (s *Service) Update(ctx context.Context, caller *user.User, ref Reference, description *string, source *Source, origin *Origin, health *string, limits *Limits, auto *Autoscale, place *Placement) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}

	// The source and its origin fields are one decision: checkOrigin
	// judges them together, and an app that names an image its source
	// ignores is exactly what it exists to refuse.
	if source != nil || origin != nil {
		next := Source(a.Source)
		if source != nil {
			next = *source
		}
		if !next.Valid() {
			return nil, ErrUnknownSource
		}
		o := Origin{Image: a.SourceImage, Repo: a.SourceRepo, Ref: a.SourceRef, Dockerfile: a.SourceDockerfile}
		if origin != nil {
			o = *origin
		}
		if err := checkOrigin(next, &o); err != nil {
			return nil, err
		}
		if err := user.Require(caller, RoleToDeploy(next)); err != nil {
			return nil, err
		}
		source, origin = &next, &o
	}

	// The health check path, which is neither a source decision nor a
	// placement one: it is how Traefik decides a container behind a
	// name is worth traffic. Checked rather than trusted because it is
	// interpolated into a dynamic YAML document and a container label —
	// see ValidHealthPath.
	if health != nil && !ValidHealthPath(*health) {
		return nil, ErrInvalidHealthPath
	}

	// The ceiling, checked here because a number the Engine refuses
	// would otherwise be found out one container at a time, on whatever
	// machine tried it, minutes after somebody typed it.
	if limits != nil && !limits.Valid() {
		return nil, ErrInvalidLimits
	}

	// And the rule, refused here for the same reason: one without a
	// ceiling is a loop of requests turning into a loop of replicas
	// until the machine has nothing left, which is a worse outage than
	// the one autoscaling was turned on to avoid.
	if auto != nil && !auto.Valid() {
		return nil, ErrInvalidAutoscale
	}

	if _, err := s.Repo().Update(ctx, a.ID, description, source, origin, health, limits, auto); err != nil {
		return nil, err
	}

	// A ceiling is the one thing about a container the Engine can
	// change while it runs, so it takes effect now rather than on the
	// next deploy. Every other setting on this screen is baked into a
	// container at create time and waits for one.
	if limits != nil {
		if err := s.orch.cap(ctx, &a.App, *limits); err != nil {
			return nil, err
		}
	}

	// Where it runs and where its traffic arrives, which are a
	// different kind of change from the rest of this and are checked
	// together: the machine that serves an app has to be one of the
	// machines running it, or its edge would balance across a set it is
	// not in.
	if place != nil {
		next := Source(a.Source)
		if source != nil {
			next = *source
		}
		if err := s.replace(ctx, a, *place, next); err != nil {
			return nil, err
		}
	}
	return s.Resolve(ctx, caller, ref, user.RoleMember)
}

// Placement is where an app runs and where its traffic arrives.
//
// Two fields because they are two decisions. Scaling an app out is
// adding a machine to Nodes; moving where its record points is changing
// Edge. Conflating them would mean a name that moves every time a
// replica is added.
type Placement struct {
	// Nodes are the machines it runs on, by name. Empty means the ones
	// it already has, which is what changing only the count sends.
	Nodes []string
	// Replicas is how many copies run in total, spread over Nodes.
	//
	// A number for the app rather than one per machine, because that is
	// how scale is thought about — "run four of these" — and because a
	// count per machine would be a third decision to keep in step with
	// the other two by hand. Zero keeps however many it has, so adding
	// a machine does not silently change the count.
	//
	// Never fewer than there are machines: a machine an app was placed
	// on and given nothing to run is a machine somebody put it on for
	// no effect. Asking for that is asking for fewer machines.
	Replicas int
	// Spread makes the app follow the cluster: it runs on every machine
	// there is, and is re-spread whenever one is added or taken away.
	// Nil leaves the switch as it is.
	//
	// It is not a third way of naming machines — it is the answer to
	// "which machines" being made once instead of every time the
	// cluster changes shape. Naming machines alongside it turns it off,
	// because that is choosing by hand.
	Spread *bool
}

// replace applies a placement.
//
// **It takes effect now**, on every machine: a worker is woken and
// reconciles in the second after this, and this machine's own copies
// are brought into line here. Before, the second half did not exist —
// the same request meant "in ten seconds" on a worker and "whenever
// somebody next deploys" on the control plane, which is two answers to
// one question.
//
// A copy this machine no longer runs is stopped rather than left, and
// that is not the outage it looks like. The proxy is built out of the
// rows this rewrites, so a name loses its old backends the moment they
// stop being what should run — keeping the container alive would keep
// nothing serving, and would leave one holding its memory under a name
// nothing on the instance points at. The gap while a machine gaining an
// app pulls its image is the same gap a move between two workers has
// always had.
func (s *Service) replace(ctx context.Context, a *Scoped, p Placement, source Source) error {
	nodes := dedupe(p.Nodes)

	// Whether it follows the cluster, decided before which machines it
	// is on — because when it does, that is the answer.
	spread := a.Spread
	if p.Spread != nil {
		spread = *p.Spread
	}
	if len(nodes) > 0 {
		// Naming machines is choosing by hand, which is the opposite of
		// following the cluster. Turned off rather than refused: what
		// somebody just said is what they want, and a refusal here
		// would be an error message in front of an obvious intent.
		spread = false
	}

	if spread {
		var err error
		if nodes, err = s.Repo().EverySlug(ctx, 0); err != nil {
			return err
		}
	}
	if len(nodes) == 0 {
		// Naming no machines means the ones it has. That is what
		// changing only the count sends, and it is the difference
		// between scaling up and scaling out.
		nodes = a.Nodes()
	}
	if len(nodes) == 0 {
		return fmt.Errorf("%w: an app has to run somewhere", ErrNoSuchNode)
	}
	for _, n := range nodes {
		if err := s.checkPlacement(ctx, n, source); err != nil {
			return err
		}
	}

	// What was asked for, which is not what there is. Naming no number
	// keeps whatever was asked for before — and for almost every app
	// that is "one per machine", which is the answer a row count cannot
	// hold. See App.Scale.
	scale := p.Replicas
	if scale == 0 {
		scale = a.Scale
	}
	copies := scale
	if copies == 0 {
		copies = len(nodes)
	}
	// What this machine is running now, read before the rows are
	// rewritten: a replica's container id is the only record of what
	// that copy was, and rewriting the rows is what destroys it.
	here, err := s.Repo().ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	before := a.ReplicasOn(here)

	if err := s.Repo().SetNodes(ctx, a.ID, nodes, scale, copies, spread); err != nil {
		return err
	}

	// **Scaling takes effect now, in both directions.** A worker is
	// woken and reconciles in the second after this; before this the
	// control plane did neither, so the same request meant "in ten
	// seconds" on one machine and "whenever somebody next deploys" on
	// the other.
	after, err := s.Repo().ScopedByID(ctx, a.ID)
	if err != nil {
		return err
	}
	s.orch.retire(ctx, retired(before, after.ReplicasOn(here)))
	s.orch.scaleLocally(a.ID)

	// The set an open deploy is waiting on has just changed, and one of
	// the machines it was waiting for may have been what was left. A
	// deploy is closed by a report, and no report is coming for a
	// machine that is no longer running this app.
	if err := s.orch.settleOpen(ctx, a.ID); err != nil {
		return err
	}
	// Which machines run it has just changed, so which containers the
	// proxy should name has too.
	s.announceRoutes()
	return nil
}

// retired is the copies that were here and are not any more: a machine
// taken off the app, or a scale lowered past their ordinal.
func retired(before, after []Replica) []Replica {
	keep := make(map[int]bool, len(after))
	for _, r := range after {
		keep[r.Ordinal] = true
	}
	var gone []Replica
	for _, r := range before {
		if !keep[r.Ordinal] {
			gone = append(gone, r)
		}
	}
	return gone
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// checkPlacement is what an app has to be to run somewhere other than
// the control plane.
//
// One refusal left, and it is not about the app: **a source that builds
// needs somewhere to push to**. The build happens on the control plane
// wherever the app runs — that is where the builder and the repository
// credentials are — and its result reaches another machine only through
// this instance's own registry, which exists once there is a domain.
// Without one the image would be loaded into this machine's Engine and
// the machine that is to run it would have nowhere to pull it from.
//
// It is checked here as well as in the build because the two failures
// are different sizes: here it is a sentence in front of somebody
// making the decision, and there it is a deploy that fails on a box
// nobody is looking at.
//
// A **domain** for the app itself is no longer one. Every machine runs
// its own edge, so an app answers at its name wherever it is — what has
// to follow it is the DNS record, which points at a machine rather than
// at an instance. That is the operator's to move, and it is why the
// app's response carries the address it should point at.
func (s *Service) checkPlacement(ctx context.Context, nodeSlug string, source Source) error {
	if nodeSlug == node.ControlPlaneSlug {
		// Coming back to the control plane is always allowed: it is
		// where everything works.
		return nil
	}
	if source.Builds() && s.orch.registryHost(ctx) == "" {
		return fmt.Errorf("%w: it is built here, and a build only reaches another machine through this instance's own registry — which needs a domain. Set one in the instance settings, or run an image from a registry, which can be placed anywhere",
			ErrNotPlaceable)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.orch.Retire(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("stop the app's container: %w", err)
	}
	return a, s.Repo().Delete(ctx, a.ID)
}

// DeleteAppsInProject and DeleteAppsInEnvironment are
// project.AppTeardown: what deleting a project or an environment calls
// to take the apps under it out of service first.
//
// They take no caller. Authorization happened above — nobody reaches
// these without having been allowed to delete the thing that contains
// them — and re-deriving it here from an app's own organization would
// ask a question already answered.
func (s *Service) DeleteAppsInProject(ctx context.Context, projectID int64) error {
	apps, err := s.Repo().ListForProject(ctx, projectID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

func (s *Service) DeleteAppsInEnvironment(ctx context.Context, environmentID int64) error {
	apps, err := s.Repo().ListForEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

// deleteAll retires and removes apps one at a time, stopping at the
// first failure.
//
// Stopping is deliberate. Retire already refuses to return success while
// a container it could not remove is still running, and carrying on past
// that would delete the rest of the rows and leave whoever asked with a
// container nothing on the instance names any more. What has been
// deleted stays deleted, and the delete above is refused — so a retry
// resumes rather than starting over.
func (s *Service) deleteAll(ctx context.Context, apps []*App) error {
	for _, a := range apps {
		if err := s.orch.Retire(ctx, a.ID); err != nil {
			return fmt.Errorf("stop app %q's container: %w", a.Name, err)
		}
		if err := s.Repo().Delete(ctx, a.ID); err != nil {
			return err
		}
		// Its history goes with it. Ids come from a sequence and are
		// reused across tables, so leaving these behind would give some
		// later app a chart of a stranger's.
		if err := s.metrics.Forget(ctx, metrics.KindApp, a.ID); err != nil {
			return err
		}
	}
	return nil
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

// Env returns the app's own variables plus the effective environment its
// container actually runs with: the project's, overridden by the
// environment's, overridden by the app's, each value labelled with the
// level that won it.
//
// Without this there is no way to see what an app is configured with —
// which is what made replacing the whole map so easy to do by accident.
func (s *Service) Env(ctx context.Context, caller *user.User, ref Reference) (envvar.Map, []envvar.Resolved, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.projects.Repo().ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.projects.EnvironmentRepo().ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, nil, err
	}
	// The same layers the container is built from, in the same order —
	// this is the screen that answers "where did DATABASE_URL come
	// from", and it can only answer it if it is looking at what the
	// deploy looks at.
	var stores envvar.Map
	if s.datastores != nil {
		if stores, err = s.datastores.VarsForApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	var buckets envvar.Map
	if s.objectStores != nil {
		if buckets, err = s.objectStores.VarsForApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	resolved := envvar.Resolve(
		envvar.Layer{Source: envvar.SourceProject, Vars: p.Env},
		envvar.Layer{Source: envvar.SourceEnvironment, Vars: e.Env},
		envvar.Layer{Source: envvar.SourceDatastore, Vars: stores},
		envvar.Layer{Source: envvar.SourceObjectStore, Vars: buckets},
		envvar.Layer{Source: envvar.SourceApp, Vars: a.Env})
	return a.Env, resolved, nil
}

// requireEnvRole is the role writing an app's environment takes.
//
// For an app that runs a published image it is a member's: the variables
// are the container's, read by whatever that image already does with
// them.
//
// For an app that builds, they are also *build input*. Railpack reads
// the environment to work out how to build the repository, and turns
// RAILPACK_INSTALL_CMD, RAILPACK_BUILD_CMD and RAILPACK_START_CMD into
// the commands the build runs — inside the privileged builder, on this
// host. Writing them is therefore the same act as building, and it takes
// the same role: an app's own variables win the merge over its
// environment's and its project's, so a member who could write them
// could decide what an admin's app builds and runs.
func (s *Service) requireEnvRole(caller *user.User, a *Scoped) error {
	return user.Require(caller, RoleToDeploy(Source(a.Source)))
}

// SetEnv replaces the app's own variables, deleting any key not present.
// They are layered on top of (and override) its environment's and
// project's.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, ref Reference, env envvar.Map) (*Scoped, error) {
	// Resolved as a member first, so someone who cannot write this app's
	// environment still gets the answer an unknown app gets rather than
	// two different refusals. See requireEnvRole for the check after.
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().SetEnv(ctx, a.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, ref Reference, set envvar.Map, unset []string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().MergeEnv(ctx, a.ID, set, unset)
}

// Deploy accepts a redeploy of an app from a tag already pushed to its
// registry path, and returns the deployment recording it. The work runs
// detached — see Orchestrator.Start — so the caller can stop waiting
// without stopping the deploy.
//
// An empty tag is the source's to fill in: "latest" for an image, the
// stored ref for a source that builds. Defaulting it here turned every
// build with no tag asked for into a build of a branch called latest.
func (s *Service) Deploy(ctx context.Context, caller *user.User, ref Reference, tag string) (*Scoped, *Deployment, error) {
	// Resolved as a member first, so someone outside the organization
	// gets the same 404 an unknown app gets rather than learning it
	// exists. The source's own requirement is checked after.
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	if err := user.Require(caller, RoleToDeploy(Source(a.Source))); err != nil {
		return nil, nil, err
	}
	deployment, err := s.orch.Start(ctx, a.ID, tag)
	if err != nil {
		return nil, nil, err
	}
	return a, deployment, nil
}

// WaitForDeploys blocks until every deploy the orchestrator has running
// has finished. For tests: a deploy started by a webhook outlives the
// request that started it, and a test that drops its schema while one
// is still writing deadlocks in Postgres.
func (s *Service) WaitForDeploys() { s.orch.Wait() }

// WaitForDeployment blocks until a deployment finishes or ctx is done.
// Abandoning the wait does not abandon the deploy.
func (s *Service) WaitForDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.WaitFor(ctx, a.ID, deploymentID)
}

// Deployment reads one of an app's deployments.
func (s *Service) Deployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	d, err := s.Repo().DeploymentByID(ctx, a.ID, deploymentID)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}
	// The one row somebody polls while they watch a deploy, so it is
	// the one that has to be able to say the wait is over even though
	// the status has not changed.
	if err := s.markStalled(ctx, a, []*Deployment{d}); err != nil {
		return nil, err
	}
	d.Deletable = d.Done() || d.Stalled != nil
	return d, nil
}

// DeleteDeployment removes one deploy's record — and, when that deploy
// is the one the app is running, the container it produced.
//
// **Deleting the live deployment takes the app down.** That is the
// point of it. An app whose running version has to go *now* — a
// compromised image, something doing what it should not — should not
// force somebody to delete the app and lose its domains, its
// environment and everything it is attached to. The app stays, with all
// of it, and comes back on the next deploy.
//
// Deleting any other row removes a record and nothing else: the
// container it produced is long gone, and what goes with the row is the
// build log, which is most of its bytes. The image stays in the
// registry, which needs a garbage collection pass Cubeship does not run
// — the same thing that is true when an app itself is deleted.
//
// One is refused: a deploy that has not finished, because the
// orchestrator is still writing to that row.
//
// The container goes before the row, because Docker has no rollback. A
// failure there leaves the record standing, which a retry finishes; the
// reverse would leave a container running with nothing naming it.
//
// The role is the one that deploys this app: somebody who may replace
// what is running may take it off.
func (s *Service) DeleteDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) error {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return err
	}
	if err := user.Require(caller, RoleToDeploy(Source(a.Source))); err != nil {
		return err
	}
	d, err := s.Repo().DeploymentByID(ctx, a.ID, deploymentID)
	if err != nil {
		return ErrDeploymentNotFound
	}
	if !d.Done() {
		// Unless nothing is coming for it. A deploy waiting on a
		// machine that has stopped answering has nobody writing to that
		// row, and refusing would make the record permanent — see
		// Stall.
		if err := s.markStalled(ctx, a, []*Deployment{d}); err != nil {
			return err
		}
		if d.Stalled == nil {
			return ErrDeploymentRunning
		}
	}

	live, err := s.liveDeployment(ctx, a)
	if err != nil {
		return err
	}
	if d.ID == live {
		if err := s.orch.Retire(ctx, a.ID); err != nil {
			return fmt.Errorf("stop the app's container: %w", err)
		}
		here, err := s.Repo().ControlPlaneID(ctx)
		if err != nil {
			return err
		}
		// Every copy on this machine, because Retire stopped every one
		// of them. A record left naming a container that is gone is one
		// the next deploy would try to stop again.
		for _, r := range a.ReplicasOn(here) {
			if err := s.Repo().UpdateContainer(ctx, a.ID, here, r.Ordinal, "", "", 0, StatusDown); err != nil {
				return err
			}
		}
	}

	removed, err := s.Repo().DeleteDeployment(ctx, a.ID, deploymentID)
	if err != nil {
		return err
	}
	if !removed {
		return ErrDeploymentNotFound
	}
	return nil
}

// liveDeployment is the deploy the app is running, or 0 for an app that
// is not running anything.
//
// The container is what decides. An app with none is running no
// deployment whatever its history says — which is the state deleting
// the live one leaves it in, and the reason the record below it does
// not quietly inherit the title.
func (s *Service) liveDeployment(ctx context.Context, a *Scoped) (int64, error) {
	if !a.HasContainer() {
		return 0, nil
	}
	id, _, err := s.Repo().CurrentDeployment(ctx, a.ID)
	return id, err
}

// MaxDeploymentHistory bounds how much of an app's history a listing
// returns. Deploy history grows without limit; nobody reads past the
// recent ones.
const MaxDeploymentHistory = 50

// Deployments returns an app's recent deploy history, newest first,
// each marked with whether its record may be removed and whether it is
// the one the app is running — which is what makes removing it a
// different act. See DeleteDeployment.
func (s *Service) Deployments(ctx context.Context, caller *user.User, ref Reference) ([]*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	history, err := s.Repo().ListDeployments(ctx, a.ID, MaxDeploymentHistory)
	if err != nil {
		return nil, err
	}
	live, err := s.liveDeployment(ctx, a)
	if err != nil {
		return nil, err
	}
	for _, d := range history {
		d.Live = d.ID == live
	}
	if err := s.markStalled(ctx, a, history); err != nil {
		return nil, err
	}
	for _, d := range history {
		// A deploy that has not finished is refused for deletion,
		// because the orchestrator is still writing to that row. A
		// stalled one is the exception: nothing is writing to it and
		// nothing is coming, so refusing would make the record
		// permanent.
		d.Deletable = d.Done() || d.Stalled != nil
	}
	return history, nil
}

// markStalled fills in which of these deploys is waiting on a machine
// that has gone quiet.
//
// **It is the machine's silence that decides, not the deploy's age.** A
// deploy started two minutes ago, onto a box that died this morning, is
// stalled now — nothing about waiting another quarter of an hour would
// make that truer. And the silence has to be long enough that a reboot
// or a blip has had time to end: `unreachable` is three missed passes,
// which is the right patience for taking a machine out of a load
// balancer and nowhere near enough to conclude a rollout is never
// happening.
//
// The cluster is asked once, and only when there is an unfinished
// deploy to ask about — on an instance of one box, or one where every
// deploy finished, this costs nothing.
func (s *Service) markStalled(ctx context.Context, a *Scoped, history []*Deployment) error {
	open := false
	for _, d := range history {
		if !d.Done() {
			open = true
		}
	}
	if !open || s.remote == nil {
		return nil
	}
	quiet, err := s.remote.Quiet(ctx, StuckAfter)
	if err != nil {
		// The cluster's own state is not something a deploy history
		// should fail on. Without it nothing is reported stalled, which
		// is the answer this had before there was one.
		log.Printf("deployments of %s: could not read which machines are answering: %v", ReferenceOf(a), err)
		return nil
	}

	for _, d := range history {
		if d.Done() {
			continue
		}
		var waiting []string
		for _, r := range a.Replicas {
			// A machine that has taken this deploy is not one anybody
			// is waiting for, whatever its state.
			if r.Deploy == d.ID && r.Running() {
				continue
			}
			if quiet[r.NodeID] {
				waiting = append(waiting, r.NodeSlug)
			}
		}
		if len(waiting) > 0 {
			d.Stalled = &Stall{Waiting: waiting}
		}
	}
	return nil
}

// Logs returns an app's container output. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (s *Service) Logs(ctx context.Context, caller *user.User, ref Reference, server, tail string) (io.ReadCloser, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	// **A log belongs to one container, so it belongs to one machine.**
	// An app spread over three of them has three logs and no combined
	// one — interleaving them would need a clock the three do not share
	// — so the caller names a machine and gets that machine's, and
	// naming none gets the one its traffic arrives at.
	r, err := a.pick(server)
	if err != nil {
		return nil, err
	}
	// An app on another machine has a log, and it is on that machine.
	// The request goes down the channel that machine's own poll opens
	// and waits there — see internal/node — so what comes back is the
	// same bytes a local log is, already demultiplexed, and nothing
	// above this line knows which machine answered.
	if r.NodeSlug != node.ControlPlaneSlug {
		return s.remoteLogs(ctx, r, tail)
	}
	return s.orch.Logs(ctx, a.ID, tail)
}

// pick resolves which of an app's copies a request means.
//
// Naming none takes the first, which is ordinal 1 on the lowest machine
// id — an order that is stable between two reads, so "the log" means
// the same copy twice running. Every copy is equal now that no single
// machine is the one traffic arrives at, so there is no better first
// than a deterministic one.
func (a *App) pick(server string) (Replica, error) {
	if server == "" {
		if len(a.Replicas) == 0 {
			return Replica{}, ErrNoContainer
		}
		return a.Replicas[0], nil
	}
	for _, r := range a.Replicas {
		if r.NodeSlug == server {
			return r, nil
		}
	}
	return Replica{}, fmt.Errorf("%w: this app does not run on %s", ErrNoSuchNode, server)
}

// remoteLogs asks the machine an app is on for its log.
//
// Its refusals are about what is true rather than about what is
// allowed: an app that has never run there has no container to read,
// and a machine that does not answer is one nothing here can make
// answer.
func (s *Service) remoteLogs(ctx context.Context, r Replica, tail string) (io.ReadCloser, error) {
	if s.remote == nil {
		return nil, fmt.Errorf("%w: this daemon has no way to reach it", ErrRemote)
	}
	if r.Container == "" {
		return nil, ErrNoContainer
	}
	out, err := s.remote.Logs(ctx, r.NodeID, r.Container, tail)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRemote, err)
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

// DeployOnPush starts a deploy for every app in an organization that
// builds from this repository at this branch, and reports how many.
//
// It authorizes nothing, deliberately: the caller is a webhook GitHub
// signed, not a person. What stands in for a role check is the
// signature, and the fact that this instance only receives events for
// the repositories its own installation has been given.
//
// An app with no ref of its own deploys on a push to any branch. That is
// what "track the default branch" means without anybody having to name
// it, and naming a ref is how you opt out.
func (s *Service) DeployOnPush(ctx context.Context, fullName, branch string) (int, error) {
	apps, err := s.Repo().BuildingFromRepository(ctx, fullName, branch)
	if err != nil {
		return 0, err
	}

	started := 0
	for _, a := range apps {
		// The branch, not a tag: for a building source the deploy's
		// argument is which commit-ish to build.
		if _, err := s.orch.Start(ctx, a.ID, branch); err != nil {
			// One app refusing must not stop the others. A repository
			// with four apps on it should deploy the three that can.
			log.Printf("deploy on push: %s: %v", ReferenceOf(a), err)
			continue
		}
		started++
	}
	return started, nil
}

// AddDomain gives an app another name to answer at.
//
// The host is normalised the way a browser sends one — lowercase, no
// trailing dot — because that is what Traefik matches against, and a
// name stored differently is a name that never matches.
//
// Port 0 means DefaultPort. See Domain.Port for why nothing reads it
// off the image.
func (s *Service) AddDomain(ctx context.Context, caller *user.User, ref Reference, host string, port int) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}

	host = NormalizeHost(host)
	if host == "" {
		return nil, ErrHostRequired
	}
	if !ValidHost(host) {
		return nil, ErrBadHost
	}
	if taken, err := s.instanceOwnsHost(ctx, host); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrHostIsTheInstance
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}

	if _, err := s.Repo().AddDomain(ctx, a.ID, host, port); err != nil {
		// The unique index is the authority. Two apps answering at one
		// name would give Traefik two answers, and which it picked
		// would be a detail of label ordering.
		if database.IsUniqueViolation(err) {
			return nil, ErrDomainTaken
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}

// instanceOwnsHost reports whether a name is one the daemon already
// routes to itself.
//
// The domain is the dashboard and the API; registry.<domain> is the
// registry. Both get Traefik routers of their own, and a router the
// unique index knows nothing about is exactly the collision it cannot
// catch — the app would be created, and one of the two would quietly
// stop answering after a deploy.
func (s *Service) instanceOwnsHost(ctx context.Context, host string) (bool, error) {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return false, err
	}
	domain := settings.APIHostFor(values.Get(settings.Domain))
	if domain == "" {
		return false, nil
	}
	return host == NormalizeHost(domain) ||
		host == NormalizeHost(settings.RegistryHostFor(values.Get(settings.Domain))), nil
}

// SetDomainPort changes what one of an app's names reaches.
func (s *Service) SetDomainPort(ctx context.Context, caller *user.User, ref Reference, domainID int64, port int) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}
	if err := s.Repo().SetDomainPort(ctx, a.ID, domainID, port); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}

// RemoveDomain takes a name off an app.
//
// The container keeps the labels it was created with, so the name goes
// on being served until the app is redeployed. That is the same rule
// every other routing change follows, and it is why this does not stop
// anything by itself.
func (s *Service) RemoveDomain(ctx context.Context, caller *user.User, ref Reference, domainID int64) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.Repo().RemoveDomain(ctx, a.ID, domainID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}
