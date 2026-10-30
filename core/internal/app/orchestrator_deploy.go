package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/dockerx"
)

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
	d, err := o.apps.UnscopedDeployment(ctx, deploymentID)
	if err != nil {
		return fmt.Errorf("read deployment identity: %w", err)
	}
	if d == nil {
		return errors.New("read deployment identity: deployment not found")
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
	setPhase := func(phase string) {
		o.phases.Store(deploymentID, phase)
		if err := o.apps.SetDeploymentPhase(context.Background(), deploymentID, phase); err != nil {
			log.Printf("deploy %d: phase %s: %v", deploymentID, phase, err)
		}
	}
	setPhase(DeploymentPhaseResolving)

	// Two deadlines rather than one: a build that took twenty minutes
	// must not leave the swap with none of its own.
	resolveCtx, cancelResolve := context.WithTimeout(ctx, BuildTimeout)
	if Source(a.Source).Builds() && d.GitSHA == "" {
		branch := tag
		if branch == "" {
			branch = a.SourceRef
		}
		lookupCtx, cancelLookup := context.WithTimeout(resolveCtx, 30*time.Second)
		username, token, err := o.cloneCredentials(lookupCtx, a)
		if err == nil {
			var sha, resolvedRef string
			sha, resolvedRef, err = buildkit.ResolveCommit(lookupCtx, a.SourceRepo, branch, username, token)
			if err == nil {
				if saveErr := o.apps.SetDeploymentGitIdentity(lookupCtx, deploymentID, sha, resolvedRef); saveErr == nil {
					d.GitSHA, d.GitBranch = sha, resolvedRef
				} else {
					log.Printf("deploy %d: could not record Git commit: %v", deploymentID, saveErr)
				}
			}
		}
		cancelLookup()
		if err != nil {
			fmt.Fprintf(logs, "Could not read the Git commit: %v\n", err)
		}
	}
	image, err := source.Resolve(resolveCtx, a, tag, d.GitSHA, logs)
	timedOut := errors.Is(resolveCtx.Err(), context.DeadlineExceeded)
	cancelResolve()
	if timedOut {
		return fmt.Errorf("resolve image: the build did not finish within %s", BuildTimeout)
	}
	if err != nil {
		return fmt.Errorf("resolve image: %w", err)
	}
	deployCtx, cancel := context.WithTimeout(ctx, DeployTimeout)
	if err := o.apps.SetDeploymentImage(deployCtx, deploymentID, image.Ref); err != nil {
		log.Printf("deploy %s: could not record the resolved image: %v", appName, err)
	}

	env, err := o.inheritedEnv(deployCtx, &a.App)
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
	here, err := o.apps.ControlPlaneID(deployCtx)
	if err != nil {
		return err
	}
	env = deploymentEnv(a, d, env, 1, here)
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
		cancel()
		if err := o.apps.SetDeploymentPhase(context.Background(), deploymentID, DeploymentPhaseWaiting); err != nil {
			log.Printf("deploy %d: waiting phase: %v", deploymentID, err)
		}
		return errPlaced
	}

	// A built image is already in the Engine's store — this deploy is
	// what put it there. Pulling would look for it in a registry that
	// has never heard of it.
	if !image.Local {
		setPhase(DeploymentPhasePulling)
		if err := o.docker.PullImage(deployCtx, image.Ref, image.Auth); err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
	}
	setPhase(DeploymentPhasePredeploy)
	if err := o.predeploy(ctx, a, image, env, logs); err != nil {
		cancel()
		return err
	}
	cancel()
	deployCtx, cancel = context.WithTimeout(ctx, DeployTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	base := resourceName(ref)
	setPhase(DeploymentPhaseSwapping)

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
		if err := o.swap(deployCtx, a, replica, image, env, labels, base, deploymentID); err != nil {
			return err
		}
	}
	return nil
}

func (o *Orchestrator) predeploy(ctx context.Context, a *Scoped, image Image, env envvar.Map, logs io.Writer) error {
	p := a.Predeploy
	if p.Empty() {
		return nil
	}
	runner, ok := o.docker.(oneShotDocker)
	if !ok {
		return errors.New("pre-deploy command is configured but the Docker engine cannot run one-shot containers")
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	opts := dockerx.ContainerOpts{
		Image: image.Ref, Env: envvar.Slice(env), Network: Network,
		AlsoNetworks: o.mesh(ctx), CapDrop: []string{"NET_RAW"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources:   a.Limits.Resources(),
	}
	if p.Shell != "" {
		opts.Entrypoint = []string{"sh"}
		opts.Cmd = []string{"-c", p.Shell}
	} else {
		opts.Entrypoint = nil
		opts.Cmd = append([]string(nil), p.Args...)
	}
	fmt.Fprintf(logs, "Running pre-deploy command\n")
	out, code, err := runner.RunOneShot(hookCtx, opts)
	out = redactPredeployOutput(out, env)
	if out != "" {
		_, _ = io.WriteString(logs, out)
		if !strings.HasSuffix(out, "\n") {
			_, _ = io.WriteString(logs, "\n")
		}
	}
	if err != nil {
		if errors.Is(hookCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("pre-deploy command timed out after %s", timeout)
		}
		return fmt.Errorf("run pre-deploy command: %w", err)
	}
	if hookCtx.Err() != nil {
		return fmt.Errorf("pre-deploy command timed out after %s", timeout)
	}
	if code != 0 {
		return fmt.Errorf("pre-deploy command exited with status %d", code)
	}
	return nil
}

func redactPredeployOutput(out string, env envvar.Map) string {
	for _, value := range env {
		if value == "" {
			continue
		}
		out = strings.ReplaceAll(out, value, "[REDACTED]")
	}
	return out
}

// swap brings one copy of an app up and retires the one it replaces.
func (o *Orchestrator) swap(ctx context.Context, a *Scoped, replica Replica, image Image,
	env envvar.Map, labels map[string]string, base string, deploymentID int64,
) error {
	// A published host port is held by one container at a time, so the
	// new one could not bind it until the old one lets go.
	if len(a.Volumes) > 0 || len(a.TCPPorts) > 0 {
		return o.swapInPlace(ctx, a, replica, image, env, labels, base, deploymentID)
	}
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
		CapDrop:      []string{"NET_RAW"},
		SecurityOpt:  []string{"no-new-privileges:true"},
		AlsoNetworks: o.mesh(ctx),
		// base is the container's name without this deploy's id on the
		// end, and it is the one address another app can hold on to.
		// See app.InternalHost.
		Aliases:   []string{base},
		Resources: a.Limits.Resources(),
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that would not start")
		return fmt.Errorf("start container: %w", err)
	}

	if err := o.waitAppHealthy(ctx, newID, a); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that never became healthy")
		return fmt.Errorf("health check timed out for container %s: %w", newID, err)
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
		if err := o.stop(ctx, replica.Container, &a.App); err != nil {
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
	// Cleanup must still work after the deployment has been canceled.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := o.docker.RemoveContainer(ctx, id); err != nil {
		log.Printf("%s: could not remove container %s, it is now orphaned: %v", why, id, err)
	}
}
