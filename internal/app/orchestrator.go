package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/extregistry"
	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/traefik"
	"cubeship/internal/project"
	"cubeship/internal/settings"
)

// DockerAPI is the subset of dockerx.Client the deploy engine needs.
// *dockerx.Client satisfies it structurally, and a test supplies a fake.
type DockerAPI interface {
	PullImage(ctx context.Context, ref string, creds *dockerx.RegistryAuth) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	SetResources(ctx context.Context, id string, r dockerx.Resources) error
	IsRunning(ctx context.Context, id string) (bool, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
}

// Orchestrator runs deploys: it is the only thing in Cubeship that
// creates or retires an app's container.
type Orchestrator struct {
	db       *database.DB
	docker   DockerAPI
	apps     *Repository
	proj     *project.Repository
	envs     *project.EnvironmentRepository
	settings *settings.Service
	creds    CredentialLookup
	builder  ImageBuilder
	git      GitTokens
	// datastores is what an attached database contributes to a
	// container's environment. Nil until the daemon wires it in.
	datastores DatastoreVars
	// objectStores is the same question for an attached bucket.
	objectStores ObjectStoreVars

	// localRegistry is where the daemon pulls an app's own image from:
	// the registry reached directly, never the public name. Pulling the
	// public one would hairpin out to this host's own address and need a
	// certificate to already exist, which must not be what a deploy
	// waits on.
	localRegistry string

	// HealthCheckAttempts bounds how many observations waitHealthy takes
	// before giving up; HealthCheckSuccesses is how many of them must be
	// consecutively good for the container to count as healthy.
	HealthCheckAttempts  int
	HealthCheckSuccesses int
	HealthCheckInterval  time.Duration

	// appLocks serializes deploys per app. Without it two pushes in quick
	// succession both read the same app.ContainerID, both create a
	// container, and the loser's container is leaked while Traefik load
	// balances two versions under one router name.
	appLocks sync.Map // app id -> *sync.Mutex

	// running tracks deploys that outlive the request that started them.
	// Tests wait on it; the daemon does not.
	running sync.WaitGroup

	// meshNetwork answers whether this instance is a cluster, and with
	// what network. Nil until server.New wires one in.
	meshNetwork func(context.Context) string

	// remote is how a machine an app is placed on is reached. Only one
	// thing here uses it: telling that machine a deploy is waiting.
	remote Remote

	// routesChanged is told when a deploy has moved which containers
	// are live, so the proxy stops naming one that has just gone.
	routesChanged func()

	// builderLogin is what BuildKit authenticates to this instance's own
	// registry with, for a build that has to be pushed. Its own
	// credential rather than anybody's: the builder is machinery, and
	// what it may do there is push and pull.
	builderLogin buildkit.Login
}

// SetBuilderLogin wires in that credential. Called once, by server.New.
func (o *Orchestrator) SetBuilderLogin(username, password string) {
	o.builderLogin = buildkit.Login{Username: username, Password: password}
}

// DeployTimeout bounds a detached deploy. It is not any client's
// timeout — nobody is waiting on the connection any more — it only stops
// a wedged deploy running forever.
const DeployTimeout = 10 * time.Minute

// DatastoreVars answers what the databases attached to an app
// contribute to its environment: DATABASE_URL and its parts, for every
// datastore wired to it.
//
// An interface rather than an import, and the direction is deliberate.
// Datastores are addressed the way apps are and are attached to apps,
// so that module sits above this one and depends on it; this is the one
// thing that has to travel back down, and it travels as a question this
// package asks rather than as a package it reaches for. The daemon
// hands the implementation in at wiring time — see server.New.
//
// Nil is a legal state: a server built without the module simply has no
// databases to inherit from.
type DatastoreVars interface {
	VarsForApp(ctx context.Context, appID int64) (envvar.Map, error)
}

// ObjectStoreVars is the same seam for the buckets attached to an app:
// S3_ENDPOINT and its parts, for every attachment wired to it.
//
// A second interface rather than one list of contributors, because the
// two are not interchangeable to the screen that has to explain them —
// each is labelled with its own envvar.Source, so "where did this come
// from" answers "a database" or "a bucket" rather than "something
// attached".
type ObjectStoreVars interface {
	VarsForApp(ctx context.Context, appID int64) (envvar.Map, error)
}

// CredentialLookup answers what login an organization holds for the
// registry an image lives in. Only an external app ever needs one.
type CredentialLookup interface {
	ForImage(ctx context.Context, image string) (*extregistry.Credential, bool, error)
	// LoginFor is what the pull actually authenticates with. For most
	// registries it is what was stored; for AWS the stored value is an
	// access key and the login is fetched from it.
	LoginFor(ctx context.Context, c *extregistry.Credential) (username, password string, err error)
}

// ImageBuilder turns a repository into an image in the Engine's store.
// Only a source that builds ever needs one, so it may be nil on an
// instance that has none.
type ImageBuilder interface {
	Build(ctx context.Context, req buildkit.Request, logs io.Writer) error
	BuildPlanned(ctx context.Context, req buildkit.PlannedRequest, logs io.Writer) error
}

// GitTokens answers what credential a clone of one organization's
// repository should use. Only a private repository ever needs one, so it
// may be nil.
type GitTokens interface {
	TokenForRepository(ctx context.Context, repoURL string) (string, bool, error)
}

func NewOrchestrator(db *database.DB, d DockerAPI, cfg *settings.Service, creds CredentialLookup, builder ImageBuilder, git GitTokens, localRegistry string) *Orchestrator {
	return &Orchestrator{
		db:                   db,
		docker:               d,
		apps:                 NewRepository(db),
		proj:                 project.NewRepository(db),
		envs:                 project.NewEnvironmentRepository(db),
		settings:             cfg,
		creds:                creds,
		builder:              builder,
		git:                  git,
		localRegistry:        localRegistry,
		HealthCheckAttempts:  10,
		HealthCheckSuccesses: 3,
		HealthCheckInterval:  500 * time.Millisecond,
	}
}

// registryCredentials is what externalSource asks for the login to pull
// an image with. Nothing found means a public image, which is not an
// error: the registry itself is what refuses an anonymous pull it should
// not have served.
func (o *Orchestrator) registryCredentials(ctx context.Context, image string) (*dockerx.RegistryAuth, error) {
	if o.creds == nil {
		return nil, nil
	}
	c, found, err := o.creds.ForImage(ctx, image)
	if err != nil || !found {
		return nil, err
	}
	username, password, err := o.creds.LoginFor(ctx, c)
	if err != nil {
		return nil, err
	}
	return &dockerx.RegistryAuth{Username: username, Password: password}, nil
}

// buildFromRepository is what a building source calls. It returns the
// image the build produced and where it is.
//
// The two ways of building differ in where the recipe comes from, and
// that difference decides everything else. A Dockerfile is in the
// repository, so BuildKit clones for itself and nothing touches the
// daemon's disk. Railpack has to *read* the repository to work out how
// to build it, and that reading happens here — so the daemon clones
// first, plans, and hands BuildKit the result.
//
// **Where the result goes depends on where the app runs.** An app here
// takes the image into this machine's Engine, which is faster and needs
// no registry at all. An app on another machine takes it to the
// instance's own registry, because an image loaded here is one no other
// machine has ever heard of — see pushTarget.
func (o *Orchestrator) buildFromRepository(ctx context.Context, a *Scoped, ref string, logs io.Writer) (Image, error) {
	if o.builder == nil {
		return Image{}, ErrNoBuilder
	}
	image, push, err := o.buildTarget(ctx, a, ref)
	if err != nil {
		return Image{}, err
	}

	// A private repository needs a credential, and an organization holds
	// one only for accounts it has connected. Nothing found means a
	// public repository — letting the clone be refused is better than
	// refusing one that would have worked.
	token, err := o.cloneToken(ctx, a)
	if err != nil {
		return Image{}, err
	}

	if Source(a.Source) == SourceRailpack {
		if err := o.buildWithRailpack(ctx, a, ref, image, push, token, logs); err != nil {
			return Image{}, err
		}
		return Image{Ref: image, Local: !push.Push}, nil
	}

	target := a.SourceRepo
	if ref != "" {
		target += "#" + ref
	}
	err = o.builder.Build(ctx, buildkit.Request{
		ContextGit: target,
		Dockerfile: a.SourceDockerfile,
		Image:      image,
		Labels:     map[string]string{"cubeship.app": ReferenceOf(a).String()},
		GitToken:   token,
		Push:       push.Push,
		Registry:   push.Registry,
	}, logs)
	if err != nil {
		return Image{}, err
	}
	return Image{Ref: image, Local: !push.Push}, nil
}

// pushTarget says whether a build's result leaves this machine, and
// with what login.
type pushTarget struct {
	Push     bool
	Registry buildkit.Login
}

// buildTarget is what the result is called and where it goes.
//
// An app on this machine keeps the old answer: a name that only has to
// be readable in `docker images`, and an image the Engine already
// holds. An app anywhere else takes the app's own registry path, which
// is the one address every machine in the cluster can pull it from.
func (o *Orchestrator) buildTarget(ctx context.Context, a *Scoped, ref string) (string, pushTarget, error) {
	// Every copy of it is on this machine, so the image never has to
	// leave: loading it into this Engine is faster and needs no
	// registry at all.
	elsewhere := a.Elsewhere()
	if len(elsewhere) == 0 {
		return BuildImageName(a, ref), pushTarget{}, nil
	}
	host := o.registryHost(ctx)
	if host == "" {
		// Refused rather than built and then found to be unpushable:
		// the registry follows the instance's domain, and without one
		// there is nowhere for another machine to pull from.
		return "", pushTarget{}, fmt.Errorf("%w: %s also runs on %s, and a build has to be pushed to this instance's registry for another machine to pull it — which needs a domain",
			ErrNoRegistry, ReferenceOf(a), strings.Join(elsewhere, ", "))
	}
	tag := "latest"
	if ref != "" {
		tag = sanitizeTag(ref)
	}
	login := o.builderLogin
	login.Host = host
	return ReferenceOf(a).ImageFor(host) + ":" + tag, pushTarget{Push: true, Registry: login}, nil
}

// cloneToken is what authenticates a clone of a private repository, or
// "" for one that needs nothing.
func (o *Orchestrator) cloneToken(ctx context.Context, a *Scoped) (string, error) {
	if o.git == nil {
		return "", nil
	}
	token, _, err := o.git.TokenForRepository(ctx, a.SourceRepo)
	return token, err
}

// buildWithRailpack clones, plans and builds.
//
// The app's environment goes into the plan, not only into the container:
// Railpack reads it for the versions and commands a project pins, so two
// apps on the same repository with different NODE_VERSION are two
// different builds.
func (o *Orchestrator) buildWithRailpack(ctx context.Context, a *Scoped, ref, image string, push pushTarget, token string, logs io.Writer) error {
	fmt.Fprintf(logs, "Fetching %s\n", a.SourceRepo)
	dir, cleanupSource, err := buildkit.Clone(ctx, a.SourceRepo, ref, token)
	if err != nil {
		return err
	}
	defer cleanupSource()

	env, err := o.inheritedEnv(ctx, &a.App)
	if err != nil {
		return fmt.Errorf("resolve inherited env: %w", err)
	}

	fmt.Fprintf(logs, "Working out how to build it\n")
	plan, providers, err := buildkit.PlanRepository(dir, env)
	if err != nil {
		return err
	}
	fmt.Fprintf(logs, "Detected %s\n", strings.Join(providers, ", "))

	planDir, cleanupPlan, err := buildkit.WritePlan(plan)
	if err != nil {
		return err
	}
	defer cleanupPlan()

	return o.builder.BuildPlanned(ctx, buildkit.PlannedRequest{
		ContextDir: dir,
		PlanDir:    planDir,
		Image:      image,
		// Mount caches are shared, so they are keyed per app: two apps
		// sharing one would fight over it.
		CacheKey: ReferenceOf(a).String(),
		Push:     push.Push,
		Registry: push.Registry,
	}, logs)
}

// lockApp returns the mutex guarding deploys of one app. Keyed by id
// rather than name, since a name is only unique within its environment.
func (o *Orchestrator) lockApp(appID int64) *sync.Mutex {
	mu, _ := o.appLocks.LoadOrStore(appID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// registryHost is the public registry name, or "" while the instance has
// no domain. It is read per call because an operator configures the
// domain from the dashboard, without a restart.
func (o *Orchestrator) registryHost(ctx context.Context) string {
	values, err := o.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return settings.RegistryHostFor(values.Get(settings.Domain))
}

// Start accepts a deploy and returns immediately with the deployment
// that records it. The work runs detached, on a context of its own.
//
// Detaching is the point: a deploy takes minutes — a pull, a container
// start, several seconds of health checks — and used to run on the
// request's context, so a client that timed out or hung up killed it
// halfway, sometimes after the new container was already running. The
// caller now polls the returned deployment instead, and can stop
// watching whenever it likes.
func (o *Orchestrator) Start(ctx context.Context, appID int64, tag string) (*Deployment, error) {
	// Look up and check the source first, so asking to deploy something
	// that isn't there — or an app whose source cannot produce an image
	// at all — is an error the caller sees rather than a background
	// failure they have to go looking for.
	a, err := o.apps.ScopedByID(ctx, appID)
	if err != nil {
		return nil, ErrNotFound
	}
	source, err := o.sourceFor(a)
	if err != nil {
		return nil, err
	}
	if err := source.Check(ctx, a); err != nil {
		return nil, err
	}

	deployment, err := o.apps.StartDeployment(ctx, appID, tag)
	if err != nil {
		return nil, err
	}

	o.running.Add(1)
	go func() {
		defer o.running.Done()
		// A fresh context: the request that asked for this may already
		// be gone, and that must not matter.
		ctx, cancel := context.WithTimeout(context.Background(), DeployTimeout)
		defer cancel()
		o.run(ctx, appID, tag, deployment.ID)
	}()
	return deployment, nil
}

// run performs one detached deploy and records how it ended.
//
// It recovers, because this runs on a goroutine of its own: an
// unrecovered panic here would take the whole daemon down, and with it
// every app it is proxying — a far worse outcome than one failed deploy.
// The panic is turned into the deployment's error so it is not lost.
func (o *Orchestrator) run(ctx context.Context, appID int64, tag string, deploymentID int64) {
	status, errMsg := DeploymentSucceeded, ""

	func() {
		defer func() {
			if r := recover(); r != nil {
				status = DeploymentFailed
				errMsg = fmt.Sprintf("the deploy panicked: %v", r)
				log.Printf("deploy of app %d panicked: %v\n%s", appID, r, debug.Stack())
			}
		}()
		switch err := o.deploy(ctx, appID, tag, deploymentID); {
		case errors.Is(err, errPlaced):
			// Nothing for this machine to do: the app runs somewhere
			// else, and the machines it runs on will say how it went.
		case err != nil:
			status, errMsg = DeploymentFailed, err.Error()
			log.Printf("deploy of app %d failed: %v", appID, err)
		}
	}()

	// **A deploy is finished when every machine has it**, and this
	// machine is at most one of them. A failure is this instance's to
	// record either way — it happened here, and nothing else will say
	// so — but a success only closes the row once nothing is left to
	// wait for. See settle.
	if status == DeploymentFailed {
		if err := o.apps.FinishDeployment(ctx, deploymentID, status, errMsg); err != nil {
			log.Printf("could not record the outcome of deployment %d: %v", deploymentID, err)
		}
		return
	}
	if err := o.settle(ctx, appID, deploymentID); err != nil {
		log.Printf("could not record the outcome of deployment %d: %v", deploymentID, err)
	}
	// Whatever this did to the containers, the proxy's file is now
	// naming the wrong ones — a swap removed the container it still
	// points at. Said now rather than waited for, because the gap is a
	// 502 on every request for that name.
	if o.routesChanged != nil {
		o.routesChanged()
	}
}

// settle closes a deployment once every machine the app runs on is
// running that deployment.
//
// **Not the first machine to report.** An app on three boxes whose
// deploy is marked succeeded the moment one of them has it would report
// a rollout that is a third done as finished, and the two machines
// still pulling would look like nothing was happening. The row stays
// `pending`, which is what it means: some of this is still going on.
//
// A failure does not come through here — one machine failing fails the
// deploy immediately, above — so this only ever writes success, and
// only when there is nothing left to wait for.
func (o *Orchestrator) settle(ctx context.Context, appID, deploymentID int64) error {
	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return err
	}
	for _, r := range a.Replicas {
		if r.Deploy != deploymentID || !r.Running() {
			return nil
		}
	}
	if len(a.Replicas) == 0 {
		return nil
	}
	d, err := o.apps.UnscopedDeployment(ctx, deploymentID)
	if err != nil || d == nil || d.Done() {
		// Already closed — by a failure from another machine, or by
		// somebody deleting the row. Neither is this one's to reopen.
		return nil
	}
	return o.apps.FinishDeployment(ctx, deploymentID, DeploymentSucceeded, "")
}

// settleOpen re-asks whether an app's unfinished deploy is finished.
//
// settle is otherwise only reached from the two moments that *report*
// something — the end of a local deploy, and a machine saying what it
// did. That leaves the case where nothing is reported and the answer
// changes anyway: a machine the deploy was waiting on is taken off the
// app, and what is left is already running it. Nothing was coming to
// close that row, and a deploy that has not finished cannot even be
// deleted, so it would have sat there for the life of the instance.
func (o *Orchestrator) settleOpen(ctx context.Context, appID int64) error {
	d, err := o.apps.OpenDeployment(ctx, appID)
	if err != nil || d == nil {
		return err
	}
	return o.settle(ctx, appID, d.ID)
}

// errPlaced is how deploy says "this one is somebody else's to run".
// Not an error anybody sees: it never leaves this file, and what it
// means is that the deployment row is deliberately left open.
var errPlaced = errors.New("this app runs on another machine")

// Wait blocks until every detached deploy has finished. Tests use it;
// the daemon does not.
func (o *Orchestrator) Wait() { o.running.Wait() }

// WaitFor polls a deployment until it finishes or ctx is done. Giving up
// on the wait does not give up on the deploy — that is what detaching
// bought.
func (o *Orchestrator) WaitFor(ctx context.Context, appID, deploymentID int64) (*Deployment, error) {
	const poll = 250 * time.Millisecond
	for {
		d, err := o.apps.DeploymentByID(ctx, appID, deploymentID)
		if err != nil {
			return nil, ErrDeploymentNotFound
		}
		if d.Done() {
			return d, nil
		}
		select {
		case <-ctx.Done():
			return d, ctx.Err()
		case <-time.After(poll):
		}
	}
}

// deploy pulls imageRef, starts a container from it, waits for it to look
// healthy, and only then retires the app's previous container.
//
// imageRef is the reference the daemon itself can pull — for apps in the
// embedded registry that is the loopback-published host
// (127.0.0.1:5000/<repo>:<tag>), not the public registry.<domain> name
// the user pushes to. Pulling the public name would hairpin out to the
// VPS's own public IP and require an ACME certificate to already exist,
// which must never block a deploy.
//
// Deploys of the same app are serialized; deploys of different apps run
// concurrently.
func (o *Orchestrator) deploy(ctx context.Context, appID int64, tag string, deploymentID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ScopedByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	ref := ReferenceOf(a)
	appName := ref.String()

	// Asking the source for an image happens here, inside the detached
	// deploy, because a source that builds does its building here — and
	// nobody is holding a connection open waiting for it.
	source, err := o.sourceFor(a)
	if err != nil {
		return err
	}
	// A source that builds writes to this while it works, so the
	// deployment row is watchable rather than a blank wait. Closed
	// however the deploy ends: the last lines are the ones explaining
	// it.
	logs := newDeploymentLog(o.saveDeploymentLogs(deploymentID))
	defer logs.Close()

	image, err := source.Resolve(ctx, a, tag, logs)
	if err != nil {
		return fmt.Errorf("resolve image: %w", err)
	}
	if err := o.apps.SetDeploymentImage(ctx, deploymentID, image.Ref); err != nil {
		log.Printf("deploy %s: could not record the resolved image: %v", appName, err)
	}

	env, err := o.inheritedEnv(ctx, &a.App)
	if err != nil {
		return fmt.Errorf("resolve inherited env: %w", err)
	}

	// Every machine the app runs on is told, and this one does its own
	// work below. Everything above this line is the control plane's
	// whatever the app is on — resolving the image, recording what it
	// resolved to — and everything below it is running a container,
	// which happens on each machine that runs one.
	//
	// The deploy is left `pending` until **every** machine has reported
	// the same one running: see settle. Nothing is polled here — a
	// deploy nobody is holding a connection open for does not need a
	// second thing waiting on it.
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	local := false
	for _, r := range a.Replicas {
		if r.NodeID == here {
			local = true
			continue
		}
		// And tell it now rather than letting it find out on its next
		// poll. The machine is parked on a request this releases, so a
		// deploy starts in the second it was asked for rather than in
		// the half-minute after.
		if o.remote != nil {
			o.remote.Wake(r.NodeID)
		}
	}
	if !local {
		return errPlaced
	}

	// A built image is already in the Engine's store — this deploy is
	// what put it there. Pulling would look for it in a registry that
	// has never heard of it.
	if !image.Local {
		if err := o.docker.PullImage(ctx, image.Ref, image.Auth); err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
	}

	base := resourceName(ref)

	// **One copy at a time**, which is what makes several of them on one
	// machine a rolling deploy rather than a moment with none of them
	// serving. Each is brought up and proved healthy before the one it
	// replaces is stopped, exactly as the single copy always was — an
	// app that runs one of itself takes this loop once and cannot tell
	// the difference.
	//
	// A copy that will not come up stops the rest. The ones already
	// swapped keep the new version and the ones after it keep the old,
	// which is a split this instance reports rather than hides — see
	// App.Split — and it beats carrying on into an app that is entirely
	// the version that does not work.
	for _, replica := range a.ReplicasOn(here) {
		// Its own labels, because one of them says which copy it is.
		labels := placementLabels(appName, deploymentID, replica.Ordinal)
		if err := o.swap(ctx, a, replica, image, env, labels, base, deploymentID); err != nil {
			return err
		}
	}
	return nil
}

// swap brings one copy of an app up and retires the one it replaces.
func (o *Orchestrator) swap(ctx context.Context, a *Scoped, replica Replica, image Image,
	env envvar.Map, labels map[string]string, base string, deploymentID int64,
) error {
	appName := ReferenceOf(a).String()
	// Named for the deploy it is and the copy it is, rather than for
	// the moment it was created. A machine that is told to run a
	// placement twice has to be able to answer "am I already running
	// this", and a name with a timestamp in it can only answer "am I
	// running something".
	newName := containerNameFor(base, deploymentID, replica.Ordinal)
	if replica.Container != "" && replica.Name == newName {
		// Already this deploy's container. Nothing to do, and creating
		// one would collide on the name.
		return nil
	}

	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:         newName,
		Image:        image.Ref,
		Labels:       labels,
		Env:          envvar.Slice(env),
		Network:      Network,
		AlsoNetworks: o.mesh(ctx),
		Resources:    a.Limits.Resources(),
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that would not start")
		return fmt.Errorf("start container: %w", err)
	}

	if !o.waitHealthy(ctx, newID) {
		o.removeContainer(ctx, newID, "abandoning a container that never became healthy")
		return fmt.Errorf("health check timed out for container %s", newID)
	}

	if err := o.apps.UpdateContainer(ctx, a.ID, replica.NodeID, replica.Ordinal, newID, newName,
		deploymentID, StatusRunning); err != nil {
		// The new container is healthy but the database doesn't know
		// about it, so nothing will ever retire it. Remove it rather
		// than leave two containers answering one router.
		o.removeContainer(ctx, newID, "rolling back a deploy the database did not record")
		return fmt.Errorf("update app container: %w", err)
	}

	if replica.Container != "" && replica.Container != newID {
		if err := o.docker.StopContainer(ctx, replica.Container); err != nil {
			log.Printf("deploy %s: could not stop the previous container %s: %v", appName, replica.Container, err)
		}
		o.removeContainer(ctx, replica.Container, "retiring the previous container")
	}
	return nil
}

// removeContainer removes a container that should no longer exist. A
// failure here leaks a container, which is worth a log line: nothing else
// will ever clean it up.
func (o *Orchestrator) removeContainer(ctx context.Context, id, why string) {
	if err := o.docker.RemoveContainer(ctx, id); err != nil {
		log.Printf("%s: could not remove container %s, it is now orphaned: %v", why, id, err)
	}
}

// Logs returns the app's container log. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (o *Orchestrator) Logs(ctx context.Context, appID int64, tail string) (io.ReadCloser, error) {
	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return nil, ErrNotFound
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	// This machine's own replica, never whichever container the app has
	// somewhere. A container id from another Engine is one this Docker
	// answers "no such container" for, which reads as an app that is
	// down rather than as a log that is somewhere else.
	mine, ok := a.ReplicaOn(here)
	if !ok || mine.Container == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, mine.Container, tail)
}

// Retire stops and removes an app's container, if it has one. It is what
// deleting an app calls before the row goes: a container left running
// with no row would serve traffic that nothing knows how to stop.
func (o *Orchestrator) Retire(ctx context.Context, appID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	// Only this machine's own containers, and every one of them. A
	// replica on another machine goes when that machine next asks what
	// it should be running and does not find this app in the answer —
	// the row is being deleted, so it will not be. Reaching for it here
	// would mean stopping a container through an Engine this daemon
	// cannot see.
	for _, mine := range a.ReplicasOn(here) {
		if mine.Container == "" {
			continue
		}
		if err := o.docker.StopContainer(ctx, mine.Container); err != nil {
			log.Printf("retiring app %d: could not stop container %s: %v", appID, mine.Container, err)
		}
		// Unlike the log-and-continue cases in Deploy, this one is
		// returned: the caller is about to delete the row, and doing
		// that while a container survives is exactly the state to
		// avoid.
		if err := o.docker.RemoveContainer(ctx, mine.Container); err != nil {
			return err
		}
	}
	return nil
}

// waitHealthy reports whether a freshly started container looks healthy.
//
// It requires HealthCheckSuccesses *consecutive* running observations,
// and waits HealthCheckInterval before each one — including the first.
// Checking immediately after ContainerStart proves nothing: Docker
// reports the container running the instant the process is spawned,
// before the app has had any chance to crash. And because every container
// carries RestartPolicy: unless-stopped, a crash-looping app
// intermittently reports running, so a single positive observation can be
// pure luck; requiring a run of them raises the bar without needing
// per-app health configuration.
//
// TODO (follow-up): an actual HTTP probe against Port would be a stronger
// signal than the container's process state.
func (o *Orchestrator) waitHealthy(ctx context.Context, containerID string) bool {
	needed := o.HealthCheckSuccesses
	if needed < 1 {
		needed = 1
	}

	consecutive := 0
	for i := 0; i < o.HealthCheckAttempts; i++ {
		if o.HealthCheckInterval > 0 {
			// Waiting on ctx as well as the timer: a cancelled deploy
			// must not keep sleeping through every remaining attempt.
			select {
			case <-ctx.Done():
				return false
			case <-time.After(o.HealthCheckInterval):
			}
		}
		running, err := o.docker.IsRunning(ctx, containerID)
		if err != nil || !running {
			consecutive = 0
			continue
		}
		consecutive++
		if consecutive >= needed {
			return true
		}
	}
	return false
}

// inheritedEnv resolves the full environment a deploy of a should run
// with: the project's vars, overridden by its environment's vars,
// overridden by the app's own vars — so an app can override a value its
// environment sets, and an environment can override one its project sets,
// but never the other way around.
func (o *Orchestrator) inheritedEnv(ctx context.Context, a *App) (envvar.Map, error) {
	p, err := o.proj.ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	e, err := o.envs.ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("get environment: %w", err)
	}
	// A database's variables are read fresh at every deploy, not stored
	// on the app: an attachment made after the last deploy is exactly
	// the case this has to pick up, and the connection string is
	// derived from the datastore rather than copied anywhere.
	var stores envvar.Map
	if o.datastores != nil {
		stores, err = o.datastores.VarsForApp(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve attached datastores: %w", err)
		}
	}
	var buckets envvar.Map
	if o.objectStores != nil {
		buckets, err = o.objectStores.VarsForApp(ctx, a.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve attached object stores: %w", err)
		}
	}
	return envvar.Merge(p.Env, e.Env, stores, buckets, a.Env), nil
}

// routing pairs every name the app answers at with the port behind it.
//
// A domain with no port of its own gets DefaultPort. Nothing inspects
// the image: EXPOSE is a hint the author wrote, not a promise, and an
// image exposing several has no single answer — so guessing produced a
// container that came up answering nothing, at a name that resolved, for
// a reason nobody could see. The port is asked for instead.
func (o *Orchestrator) routing(domains []Domain) []traefik.Domain {
	out := make([]traefik.Domain, 0, len(domains))
	for _, d := range domains {
		port := d.Port
		if port == 0 {
			port = DefaultPort
		}
		out = append(out, traefik.Domain{Host: d.Host, Port: port})
	}
	return out
}

// SetMeshNetwork wires in what says whether this instance is a cluster.
//
// It answers with the cluster overlay's name when there is one and with
// nothing when there is not, and what it decides is one extra network
// on every container this deploys — the local bridge stays, because
// everything on this box already resolves the container there.
//
// A function rather than an interface because that is the whole of it,
// and `server.New` is the only caller. See internal/mesh.
func (o *Orchestrator) SetMeshNetwork(fn func(context.Context) string) { o.meshNetwork = fn }

// mesh is the network to also join, or none. Nothing is asked of the
// Engine when nobody wired one in, which is every test.
func (o *Orchestrator) mesh(ctx context.Context) []string {
	if o.meshNetwork == nil {
		return nil
	}
	if name := o.meshNetwork(ctx); name != "" {
		return []string{name}
	}
	return nil
}

// cap applies a new ceiling to every copy of an app that is already
// running, without restarting any of them.
//
// A ceiling is the one part of a container the Engine can change under
// a running process, which is what makes raising an app's memory a
// request rather than a redeploy. Nothing is pulled and nothing swaps.
//
// A copy on another machine is capped by that machine — the ceiling
// travels in its placement — so all there is to do here is wake it, the
// same way a deploy does.
//
// **Removing a limit is the one direction that waits.** The Engine
// merges an update and reads a zero as "leave that one alone", so a
// container goes back to uncapped by being created again, which is the
// app's next deploy. Until then it keeps the ceiling it has.
func (o *Orchestrator) cap(ctx context.Context, a *App, limits Limits) error {
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	for _, r := range a.Replicas {
		if r.NodeID != here {
			if o.remote != nil {
				o.remote.Wake(r.NodeID)
			}
			continue
		}
		if r.Container == "" {
			continue
		}
		if err := o.docker.SetResources(ctx, r.Container, limits.Resources()); err != nil {
			// The limit is stored either way — it is what the next
			// container is created with. What failed is the running
			// one taking it now, and that is worth saying rather than
			// logging: a cgroup the host cannot enforce is a fact
			// about the machine, not a transient error.
			return fmt.Errorf("the limit is saved, and %s did not take it: %w", r.Name, err)
		}
	}
	return nil
}
