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

	"cubeship/internal/platform/buildkit"
	"cubeship/internal/settings"
)

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

// cloneCredentials authenticates a private repository, or returns empty
// strings for a public repository.
func (o *Orchestrator) cloneCredentials(ctx context.Context, a *Scoped) (string, string, error) {
	if o.git == nil {
		return "", "", nil
	}
	return o.git.CredentialsForRepository(ctx, a.SourceRepo)
}

// buildWithRailpack clones, plans and builds.
//
// The app's environment goes into the plan, not only into the container:
// Railpack reads it for the versions and commands a project pins, so two
// apps on the same repository with different NODE_VERSION are two
// different builds.
func (o *Orchestrator) buildWithRailpack(ctx context.Context, a *Scoped, ref, image string, push pushTarget, username, token string, logs io.Writer) error {
	fmt.Fprintf(logs, "Fetching %s\n", a.SourceRepo)
	dir, cleanupSource, err := buildkit.Clone(ctx, a.SourceRepo, ref, username, token)
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
	return o.StartWithIdentity(ctx, appID, tag, DeploymentIdentity{})
}

func (o *Orchestrator) StartWithIdentity(ctx context.Context, appID int64, tag string, identity DeploymentIdentity) (*Deployment, error) {
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
	// What was asked for wins, and the app's own tag is what "no
	// argument" means. Resolved here rather than inside each source, so
	// the deployment row records the tag that actually ran: a history
	// whose rows all say "whatever was configured" cannot answer which
	// version an app was on last Tuesday.
	if tag == "" {
		tag = a.SourceTag
	}
	// An app that follows this instance's registry has no tag of its
	// own: the push is the deploy, and what it is running is whatever
	// was pushed last — a commit SHA, usually, and never "latest",
	// which nobody pushed and the registry does not hold. So "deploy
	// this app again" means the tag it is already on. Without this,
	// changing an environment variable and pressing Deploy asks for an
	// image that was never built.
	if tag == "" && Source(a.Source) == SourceRegistry {
		tag = o.runningTag(ctx, appID)
	}

	deployment, err := o.apps.StartDeploymentWithIdentity(ctx, appID, tag, identity.GitSHA, identity.GitBranch)
	if err != nil {
		return nil, err
	}

	o.running.Add(1)
	runCtx, cancel := context.WithCancel(context.Background())
	o.cancels.Store(deployment.ID, cancel)
	o.phases.Store(deployment.ID, DeploymentPhaseQueued)
	go func() {
		defer o.running.Done()
		defer o.cancels.Delete(deployment.ID)
		defer o.phases.Delete(deployment.ID)
		// A fresh context: the request that asked for this may already
		// be gone, and that must not matter. deploy gives each half its
		// own deadline inside this one.
		ctx, deadline := context.WithTimeout(runCtx, BuildTimeout+DeployTimeout+time.Hour)
		defer deadline()
		o.run(ctx, appID, tag, deployment.ID)
	}()
	return deployment, nil
}

// Cancel stops a detached deploy while it is resolving, pulling or waiting
// in a pre-deploy command. The old container is untouched until a swap.
func (o *Orchestrator) Cancel(deploymentID int64) bool {
	if phase, ok := o.phases.Load(deploymentID); ok {
		if phase.(string) == DeploymentPhaseSwapping || phase.(string) == DeploymentPhaseDone {
			return false
		}
	}
	v, ok := o.cancels.Load(deploymentID)
	if !ok {
		return false
	}
	v.(context.CancelFunc)()
	return true
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
			if errors.Is(ctx.Err(), context.Canceled) {
				status, errMsg = DeploymentFailed, "deploy cancelled"
			}
			// Nothing for this machine to do: the app runs somewhere
			// else, and the machines it runs on will say how it went.
		case err != nil:
			status, errMsg = DeploymentFailed, err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
				errMsg = "deploy cancelled"
			}
			log.Printf("deploy of app %d failed: %v", appID, err)
		}
	}()

	// **A deploy is finished when every machine has it**, and this
	// machine is at most one of them. A failure is this instance's to
	// record either way — it happened here, and nothing else will say
	// so — but a success only closes the row once nothing is left to
	// wait for. See settle.
	if status == DeploymentFailed {
		cancelled := errMsg == "deploy cancelled"
		if err := o.apps.FinishDeployment(context.Background(), deploymentID, status, errMsg, cancelled); err != nil {
			log.Printf("could not record the outcome of deployment %d: %v", deploymentID, err)
		}
		return
	}
	if err := o.settle(context.Background(), appID, deploymentID); err != nil {
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
