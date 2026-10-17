package app

import (
	"context"
	"io"
	"sync"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/extregistry"
	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
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
	PathOwner(ctx context.Context, image, path string) (dockerx.Owner, error)
	// Not used here. It is declared so that one Docker client satisfies
	// every module's view of the Engine, which is what lets `server`
	// hand the same value to all of them — the alternative is an
	// adapter per module whose only job is to widen an interface.
	ExecStream(ctx context.Context, id string, cmd []string, in io.Reader, out io.Writer) (string, int, error)
}

type oneShotDocker interface {
	RunOneShot(context.Context, dockerx.ContainerOpts) (string, int, error)
}

type policyStopper interface {
	StopContainerWithOptions(context.Context, string, time.Duration, string) error
}

type healthProber interface {
	ProbeHTTP(context.Context, string, string, int) error
	ProbeTCP(context.Context, string, int) error
}

func (o *Orchestrator) stop(ctx context.Context, id string, a *App) error {
	if s, ok := o.docker.(policyStopper); ok {
		grace, signal := a.StopGracePeriod, a.StopSignal
		if grace == 0 && signal == "" {
			grace, signal = DefaultStopGracePeriod, DefaultStopSignal
		}
		return s.StopContainerWithOptions(ctx, id, grace, signal)
	}
	return o.docker.StopContainer(ctx, id)
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

	// dataDir is where this machine's volumes keep their data. Set by
	// Service.SetDataDir.
	dataDir string

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
	cancels sync.Map // deployment id -> context.CancelFunc
	phaseMu sync.Mutex
	phases  sync.Map // deployment id -> phase

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

// DeploymentIdentity is provenance attached to a deploy initiated by a
// source control webhook. Empty fields mean the deploy was manual or came
// from an image source.
type DeploymentIdentity struct {
	GitSHA    string
	GitBranch string
}

// SetBuilderLogin wires in that credential. Called once, by server.New.
func (o *Orchestrator) SetBuilderLogin(username, password string) {
	o.builderLogin = buildkit.Login{Username: username, Password: password}
}

// DeployTimeout bounds a detached deploy once it has an image: the pull,
// the swap and the health gate. It is not any client's timeout — nobody is
// waiting on the connection any more — it only stops a wedged deploy
// running forever.
const DeployTimeout = 10 * time.Minute

// BuildTimeout bounds resolving the image, which is where a source that
// builds does all of its work: the clone, pulling the base image into the
// builder, the build, and loading the result into the Engine. A cold build
// on a 1–2 GB base image does not fit in DeployTimeout on a small VPS.
const BuildTimeout = 30 * time.Minute

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

// CredentialLookup answers what login this instance holds for the
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

// GitTokens answers what credential a clone of a connected account's
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

	// A private repository needs a credential, and this instance holds
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
		Args:       a.BuildArgs,
		Push:       push.Push,
		Registry:   push.Registry,
	}, logs)
	if err != nil {
		return Image{}, err
	}
	return Image{Ref: image, Local: !push.Push}, nil
}
