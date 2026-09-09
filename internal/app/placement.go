package app

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"cubeship/internal/node"
	"cubeship/internal/platform/traefik"
)

// Running an app on another machine.
//
// The split is between **deciding** and **doing**, and it falls where
// the two things each machine has are: the control plane has the
// database, the builder and the registry, and the machine the app is on
// has the Engine that will run it. So resolving an image happens here,
// and creating a container happens there, and what crosses between them
// is one instruction with everything in it.
//
// Nothing is pushed. The machine asks on its own loop, is handed what it
// should be running, and says how it went — so a worker that was off
// comes back to the current answer rather than to a queue of stale ones.

// containerNameFor is what a placement's container is called.
//
// The deployment's id rather than the moment of creation, because a
// machine has to be able to answer "am I already running this" from the
// name alone. A name with a timestamp in it can only answer "am I
// running something".
func containerNameFor(base string, deploymentID int64) string {
	return fmt.Sprintf("%s-%d", base, deploymentID)
}

// PlacementsFor is everything one machine should be running.
//
// One placement per app on it, built from the deployment that should be
// running — which is the newest that resolved to an image and did not
// fail, so a deploy the machine rejected rolls back to the one before it
// without anything here having to know that is what happened.
//
// An app with no deployment yet is not in the answer at all. There is
// nothing to run, and saying so would be a placement with no image in
// it for a machine to refuse.
func (s *Service) PlacementsFor(ctx context.Context, nodeID int64) ([]node.Placement, error) {
	apps, err := s.Repo().ScopedOnNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	out := make([]node.Placement, 0, len(apps))
	for _, a := range apps {
		d, err := s.Repo().DeploymentToRun(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		if d == nil {
			continue
		}
		placement, err := s.orch.PlacementFor(ctx, a, d)
		if err != nil {
			// One app that cannot be described is not a reason to
			// leave the machine with no answer about the others — and
			// a machine told about fewer apps than it runs does not
			// stop them, it only stops hearing about them.
			log.Printf("placement %s: %v", ReferenceOf(a), err)
			continue
		}
		out = append(out, placement)
	}
	return out, nil
}

// Placed records what a machine did with what it was told to run.
//
// This is where a remote deploy ends: the row that has been `pending`
// since the control plane resolved its image becomes succeeded or
// failed, and on success the app points at the container that is now
// serving it. Both of those are exactly what a local deploy writes at
// the same point — the difference is only which machine did the work.
func (s *Service) Placed(ctx context.Context, nodeID int64, results []node.Result) error {
	for _, r := range results {
		d, err := s.Repo().UnscopedDeployment(ctx, r.Deploy)
		if err != nil || d == nil {
			// A result for a deployment this instance does not have is
			// a machine that is behind, or one whose app has been
			// deleted while it was working. Neither is worth failing
			// the rest of the pass for.
			log.Printf("placement: %s reported on deploy %d, which is not here", r.App, r.Deploy)
			continue
		}
		a, err := s.Repo().ByID(ctx, d.AppID)
		if err != nil {
			continue
		}
		if a.NodeID != nodeID {
			// The app has been moved since the machine was told to run
			// it. What it did is not what this instance wants any more,
			// and the machine it now belongs to is the one whose report
			// counts.
			continue
		}

		if r.Error != "" {
			if err := s.Repo().FinishDeployment(ctx, d.ID, DeploymentFailed, r.Error); err != nil {
				return err
			}
			continue
		}
		if err := s.Repo().FinishDeployment(ctx, d.ID, DeploymentSucceeded, ""); err != nil {
			return err
		}
		if err := s.Repo().UpdateContainer(ctx, a.ID, r.Container, StatusRunning); err != nil {
			return err
		}
	}
	return nil
}

// PlacementFor is one app as an instruction another machine can act on.
//
// Everything is resolved here and nothing is a reference: the machine
// has no database to look anything up in. The environment is the merge
// of every level above the app, the labels are what the app's own
// container would carry, and the networks are the machine's bridge plus
// the cluster's overlay.
func (o *Orchestrator) PlacementFor(ctx context.Context, a *Scoped, d *Deployment) (node.Placement, error) {
	env, err := o.inheritedEnv(ctx, &a.App)
	if err != nil {
		return node.Placement{}, fmt.Errorf("resolve inherited env: %w", err)
	}
	values, err := o.settings.Load(ctx)
	if err != nil {
		return node.Placement{}, fmt.Errorf("read instance settings: %w", err)
	}
	// A login for a registry this instance holds keys to. The one
	// registry it does *not* carry is its own: a machine pulling from
	// there authenticates as itself, with the credential it already
	// has, and the control plane could not put it here anyway — only
	// the hash is stored. See node.AgentResponse.Registry.
	auth, err := o.registryCredentials(ctx, d.ImageRef)
	if err != nil {
		return node.Placement{}, fmt.Errorf("read the registry login: %w", err)
	}

	ref := ReferenceOf(a)
	base := resourceName(ref)
	networks := append([]string{Network}, o.mesh(ctx)...)

	return node.Placement{
		App:       ref.String(),
		Deploy:    d.ID,
		Container: containerNameFor(base, d.ID),
		Image:     d.ImageRef,
		Registry:  auth,
		Env:       env,
		// The same labels a container here would carry. They do nothing
		// on another machine yet — that machine's Traefik is not
		// running and its names are not routed — and they are what will
		// make it work when it is, so a container created now is one
		// that does not have to be recreated for it.
		Labels:   placementLabels(base, o.routing(a.Domains), values.HasTLS(), ref.String(), d.ID),
		Networks: networks,
	}, nil
}

// placementLabels are Traefik's, plus the two that say whose container
// this is.
//
// The Traefik ones do nothing on another machine yet — that machine's
// Traefik is not running and its names are not routed — and they are
// what will make it work when it is, so a container created now is one
// that does not have to be recreated for it.
func placementLabels(base string, domains []traefik.Domain, tls bool, app string, deploy int64) map[string]string {
	labels := traefik.Labels(base, domains, tls)
	labels[node.LabelApp] = app
	labels[node.LabelDeploy] = strconv.FormatInt(deploy, 10)
	return labels
}
