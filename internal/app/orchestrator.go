package app

import (
	"context"
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
}

// DeployTimeout bounds a detached deploy. It is not any client's
// timeout — nobody is waiting on the connection any more — it only stops
// a wedged deploy running forever.
const DeployTimeout = 10 * time.Minute

// CredentialLookup answers what login an organization holds for the
// registry an image lives in. Only an external app ever needs one.
type CredentialLookup interface {
	ForImage(ctx context.Context, orgID int64, image string) (*extregistry.Credential, bool, error)
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
	TokenForRepository(ctx context.Context, orgID int64, repoURL string) (string, bool, error)
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
func (o *Orchestrator) registryCredentials(ctx context.Context, orgID int64, image string) (*dockerx.RegistryAuth, error) {
	if o.creds == nil {
		return nil, nil
	}
	c, found, err := o.creds.ForImage(ctx, orgID, image)
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
// name the built image was given in the Engine's store.
//
// The two ways of building differ in where the recipe comes from, and
// that difference decides everything else. A Dockerfile is in the
// repository, so BuildKit clones for itself and nothing touches the
// daemon's disk. Railpack has to *read* the repository to work out how
// to build it, and that reading happens here — so the daemon clones
// first, plans, and hands BuildKit the result.
func (o *Orchestrator) buildFromRepository(ctx context.Context, a *Scoped, ref string, logs io.Writer) (string, error) {
	if o.builder == nil {
		return "", ErrNoBuilder
	}
	image := BuildImageName(a, ref)

	// A private repository needs a credential, and an organization holds
	// one only for accounts it has connected. Nothing found means a
	// public repository — letting the clone be refused is better than
	// refusing one that would have worked.
	token, err := o.cloneToken(ctx, a)
	if err != nil {
		return "", err
	}

	if Source(a.Source) == SourceRailpack {
		return image, o.buildWithRailpack(ctx, a, ref, image, token, logs)
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
	}, logs)
	if err != nil {
		return "", err
	}
	return image, nil
}

// cloneToken is what authenticates a clone of a private repository, or
// "" for one that needs nothing.
func (o *Orchestrator) cloneToken(ctx context.Context, a *Scoped) (string, error) {
	if o.git == nil {
		return "", nil
	}
	token, _, err := o.git.TokenForRepository(ctx, a.OrgID, a.SourceRepo)
	return token, err
}

// buildWithRailpack clones, plans and builds.
//
// The app's environment goes into the plan, not only into the container:
// Railpack reads it for the versions and commands a project pins, so two
// apps on the same repository with different NODE_VERSION are two
// different builds.
func (o *Orchestrator) buildWithRailpack(ctx context.Context, a *Scoped, ref, image, token string, logs io.Writer) error {
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
	// Traefik routes by host, so an app with no domain would come up
	// answering nothing. It is the one thing an app is created without.
	if a.Domain == "" {
		return nil, ErrDomainRequired
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
		if err := o.deploy(ctx, appID, tag, deploymentID); err != nil {
			status, errMsg = DeploymentFailed, err.Error()
			log.Printf("deploy of app %d failed: %v", appID, err)
		}
	}()

	if err := o.apps.FinishDeployment(ctx, deploymentID, status, errMsg); err != nil {
		log.Printf("could not record the outcome of deployment %d: %v", deploymentID, err)
	}
}

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

	// A built image is already in the Engine's store — this deploy is
	// what put it there. Pulling would look for it in a registry that
	// has never heard of it.
	if !image.Local {
		if err := o.docker.PullImage(ctx, image.Ref, image.Auth); err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
	}

	// Whether the app can be served over HTTPS is instance
	// configuration, read now rather than captured at startup: an
	// operator sets the contact address from the dashboard, and the next
	// deploy is what picks it up.
	values, err := o.settings.Load(ctx)
	if err != nil {
		return fmt.Errorf("read instance settings: %w", err)
	}

	base := resourceName(ref)
	newName := fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:    newName,
		Image:   image.Ref,
		Labels:  traefik.Labels(base, a.Domain, Port, values.HasTLS()),
		Env:     envvar.Slice(env),
		Network: Network,
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

	oldContainerID := a.ContainerID
	if err := o.apps.UpdateContainer(ctx, a.ID, newID, StatusRunning); err != nil {
		// The new container is healthy but the database doesn't know
		// about it, so nothing will ever retire it. Remove it rather
		// than leave two containers answering one router.
		o.removeContainer(ctx, newID, "rolling back a deploy the database did not record")
		return fmt.Errorf("update app container: %w", err)
	}

	if oldContainerID != "" {
		if err := o.docker.StopContainer(ctx, oldContainerID); err != nil {
			log.Printf("deploy %s: could not stop the previous container %s: %v", appName, oldContainerID, err)
		}
		o.removeContainer(ctx, oldContainerID, "retiring the previous container")
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
	if a.ContainerID == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, a.ContainerID, tail)
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
	if a.ContainerID == "" {
		return nil
	}
	if err := o.docker.StopContainer(ctx, a.ContainerID); err != nil {
		log.Printf("retiring app %d: could not stop container %s: %v", appID, a.ContainerID, err)
	}
	// Unlike the log-and-continue cases in Deploy, this one is returned:
	// the caller is about to delete the row, and doing that while the
	// container survives is exactly the state to avoid.
	return o.docker.RemoveContainer(ctx, a.ContainerID)
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
	return envvar.Merge(p.Env, e.Env, a.Env), nil
}
