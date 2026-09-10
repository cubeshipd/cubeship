package app

import (
	"context"
	"fmt"
	"log"
)

// fill makes this machine's copies of an app exist, without a deploy.
//
// **It is the loop the control plane did not have.** A worker creates
// whatever copy is missing on its next pass, because reconciling is what
// its loop is for — so asking for a third copy of an app on a worker
// took effect in ten seconds, and asking for a third copy of an app
// here wrote a row and left it without a container until somebody
// happened to redeploy. Two machines in one cluster behaving differently
// about the same request is the kind of difference nobody can hold in
// their head.
//
// It runs the deployment the app **should already be running**, not a
// new one: `DeploymentToRun` is the same question a worker is answered
// with, so a new copy comes up on exactly the version its neighbours
// are on. Nothing is built and no deployment row is written — scaling
// is not a deploy, and treating it as one would put a rollout in the
// history for a decision that changed no code.
//
// A copy that already has a container is left alone: swap answers "this
// is already this deploy's container" by name.
func (o *Orchestrator) fill(ctx context.Context, appID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ScopedByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}

	var missing []Replica
	for _, r := range a.ReplicasOn(here) {
		if r.Container == "" {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	d, err := o.apps.DeploymentToRun(ctx, appID)
	if err != nil {
		return err
	}
	if d == nil {
		// Nothing has ever run this app, so there is no version for a
		// new copy to come up on. Its first deploy creates every copy
		// at once, which is the path this one is not.
		return nil
	}

	env, err := o.inheritedEnv(ctx, &a.App)
	if err != nil {
		return fmt.Errorf("resolve inherited env: %w", err)
	}
	auth, err := o.registryCredentials(ctx, d.ImageRef)
	if err != nil {
		return fmt.Errorf("read the registry login: %w", err)
	}

	// Best effort, and the failure is a log line rather than the end of
	// this: the image is usually already in this Engine's store — a
	// copy is being added beside one that is running it — and for an
	// app that builds here it was never in a registry at all. A pull
	// that was genuinely needed and did not happen fails the create
	// below, with the Engine's own words about the image.
	if err := o.docker.PullImage(ctx, d.ImageRef, auth); err != nil {
		log.Printf("scale %s: pull %s failed, trying the image already here (%v)",
			ReferenceOf(a), d.ImageRef, err)
	}

	image := Image{Ref: d.ImageRef, Auth: auth}
	base := resourceName(ReferenceOf(a))
	for _, r := range missing {
		labels := placementLabels(ReferenceOf(a).String(), d.ID, r.Ordinal)
		if err := o.swap(ctx, a, r, image, env, labels, base, d.ID); err != nil {
			return fmt.Errorf("start copy %d: %w", r.Ordinal, err)
		}
	}

	// A copy that has just come up may be the last one an open deploy
	// was waiting for, and it is certainly a container the proxy should
	// be naming.
	if err := o.settleOpen(ctx, appID); err != nil {
		return err
	}
	if o.routesChanged != nil {
		o.routesChanged()
	}
	return nil
}

// retire stops copies of an app that this machine should no longer be
// running.
//
// The counterpart of fill, and it takes the replicas as they were
// **before** the rows were rewritten: their container ids are the only
// record of what those copies were, and deleting the rows is what
// destroys it. A container left behind here is one nothing on the
// instance names any more — invisible on every screen, holding its
// memory, and answering on the mesh under a name the proxy no longer
// sends anything to.
func (o *Orchestrator) retire(ctx context.Context, gone []Replica) {
	for _, r := range gone {
		if r.Container == "" {
			continue
		}
		o.removeContainer(ctx, r.Container, "retiring a copy this machine no longer runs")
	}
}

// scaleLocally brings this machine's copies of an app into line, in the
// background.
//
// Detached for the reason a deploy is: creating a copy may mean pulling
// an image, and nobody asking for a third replica is holding a
// connection open for that. What it is not is a deploy — no row is
// written and no history is added.
func (o *Orchestrator) scaleLocally(appID int64) {
	o.running.Add(1)
	go func() {
		defer o.running.Done()
		ctx, cancel := context.WithTimeout(context.Background(), DeployTimeout)
		defer cancel()
		if err := o.fill(ctx, appID); err != nil {
			log.Printf("scale app %d: %v", appID, err)
		}
	}()
}

// Rebalance re-spreads every app that follows the cluster.
//
// Called by `node` when a machine is added or is about to be taken away
// — the seam is node.Apps, the same direction PlacementsFor runs, and
// this module is the only one that knows what following the cluster
// means: every machine there is, with the app's own scale divided over
// them.
//
// An app that follows the cluster is one whose placement stopped being
// a decision, which is what makes doing this on its behalf safe. An app
// somebody placed by hand is not touched, and a machine with one of
// those on it still refuses to be removed.
func (s *Service) Rebalance(ctx context.Context, without int64) error {
	ids, err := s.Repo().Following(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	every, err := s.Repo().EverySlug(ctx, without)
	if err != nil {
		return err
	}
	if len(every) == 0 {
		// No machines at all is not a state this instance can be in —
		// the control plane is a row — so this is a table that has not
		// been migrated rather than a cluster to spread over.
		return nil
	}

	for _, id := range ids {
		a, err := s.Repo().ScopedByID(ctx, id)
		if err != nil {
			return err
		}
		// The app's own intent, unchanged: this re-spreads what was
		// asked for over a different number of machines, and does not
		// decide anything about how many copies there are.
		if err := s.Repo().SetNodes(ctx, a.ID, every, a.Scale, a.Copies(len(every)), true); err != nil {
			return err
		}
		if err := s.orch.settleOpen(ctx, a.ID); err != nil {
			return err
		}
		s.orch.scaleLocally(a.ID)
	}
	s.announceRoutes()
	return nil
}
