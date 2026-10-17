package app

import (
	"context"
	"cubeship/internal/node"
	"fmt"
)

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
