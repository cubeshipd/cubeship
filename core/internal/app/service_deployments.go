package app

import (
	"bytes"
	"context"
	"cubeship/internal/node"
	"cubeship/internal/user"
	"fmt"
	"io"
	"log"
)

// Deploy accepts a redeploy of an app from a tag already pushed to its
// registry path, and returns the deployment recording it. The work runs
// detached — see Orchestrator.Start — so the caller can stop waiting
// without stopping the deploy.
//
// An empty tag is the source's to fill in: "latest" for an image, the
// stored ref for a source that builds. Defaulting it here turned every
// build with no tag asked for into a build of a branch called latest.
func (s *Service) Deploy(ctx context.Context, caller *user.User, ref Reference, tag string) (*Scoped, *Deployment, error) {
	// Resolve the app and require manage access before starting a deployment.
	// The source's own requirement is checked after.
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, nil, err
	}
	if err := requireSource(caller, Source(a.Source)); err != nil {
		return nil, nil, err
	}
	if a.Status() == StatusStopped {
		if err := s.orch.StartApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	deployment, err := s.orch.Start(ctx, a.ID, tag)
	if err != nil {
		return nil, nil, err
	}
	return a, deployment, nil
}

func (s *Service) Stop(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := s.orch.StopApp(ctx, a.ID); err != nil {
		return nil, err
	}
	s.announceRoutes()
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

func (s *Service) Start(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := s.orch.StartApp(ctx, a.ID); err != nil {
		return nil, err
	}
	s.announceRoutes()
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

// WaitForDeploys blocks until every deploy the orchestrator has running
// has finished. For tests: a deploy started by a webhook outlives the
// request that started it, and a test that drops its schema while one
// is still writing deadlocks in Postgres.
func (s *Service) WaitForDeploys() { s.orch.Wait() }

// WaitForDeployment blocks until a deployment finishes or ctx is done.
// Abandoning the wait does not abandon the deploy.
func (s *Service) WaitForDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, err
	}
	return s.orch.WaitFor(ctx, a.ID, deploymentID)
}

// Deployment reads one of an app's deployments.
func (s *Service) Deployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
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
	live, err := s.liveDeployment(ctx, a)
	if err != nil {
		return nil, err
	}
	d.Live = d.ID == live
	markRestorable(a, []*Deployment{d})
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
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return err
	}
	if err := requireSource(caller, Source(a.Source)); err != nil {
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

// CancelDeployment stops an unfinished deploy and leaves the app's current
// container in place.
func (s *Service) CancelDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) error {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return err
	}
	if err := requireSource(caller, Source(a.Source)); err != nil {
		return err
	}
	d, err := s.Repo().DeploymentByID(ctx, a.ID, deploymentID)
	if err != nil {
		return ErrDeploymentNotFound
	}
	if d.Done() {
		return nil
	}
	if !s.orch.Cancel(deploymentID) {
		return ErrDeploymentRunning
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
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
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
	markRestorable(a, history)
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
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
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

// DeployOnPush starts a deploy for every matching app. Legacy callers have
// no changed-file data, so they fail open and deploy all matching apps.
func (s *Service) DeployOnPush(ctx context.Context, fullName, branch string) (int, error) {
	return s.DeployOnPushWithChanges(ctx, fullName, branch, nil, false)
}

func (s *Service) DeployOnPushWithChangesAndIdentity(ctx context.Context, fullName, branch, sha string, changed []string, complete bool) (int, error) {
	return s.deployOnPush(ctx, fullName, branch, sha, changed, complete)
}

func (s *Service) DeployOnPushWithChanges(ctx context.Context, fullName, branch string, changed []string, complete bool) (int, error) {
	return s.deployOnPush(ctx, fullName, branch, "", changed, complete)
}

func (s *Service) deployOnPush(ctx context.Context, fullName, branch, sha string, changed []string, complete bool) (int, error) {
	apps, err := s.Repo().BuildingFromRepository(ctx, fullName, branch)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, a := range apps {
		if complete && !WatchPathsMatch(a.WatchPaths, a.IgnorePaths, changed) {
			deployment, startErr := s.Repo().StartDeployment(ctx, a.ID, branch)
			if startErr == nil {
				_ = s.Repo().FinishDeployment(ctx, deployment.ID, DeploymentSkipped, "no watched path changed")
			}
			log.Printf("deploy on push: %s skipped: no watched path changed", ReferenceOf(a))
			continue
		}
		if _, err := s.orch.StartWithIdentity(ctx, a.ID, branch, DeploymentIdentity{GitSHA: sha, GitBranch: branch}); err != nil {
			log.Printf("deploy on push: %s: %v", ReferenceOf(a), err)
			continue
		}
		started++
	}
	return started, nil
}
