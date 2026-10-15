package app

import (
	"context"
	"log"
	"strings"
	"time"
)

// reconcileDocker is the subset of the Docker client Reconcile needs.
type reconcileDocker interface {
	IsRunning(ctx context.Context, id string) (bool, error)
}

// Reconcile compares each app's recorded container against real Docker
// state and corrects what this machine says about it. It runs at
// startup, when the database may describe a world from before a reboot.
//
// **This machine's own replicas, and no others.** A container on
// another machine is one this Engine has never heard of, and asking it
// would answer "no such container" for every app in the cluster that is
// somewhere else — turning a reconciler into something that marks every
// remote app down on every restart. Those machines correct their own
// records the same way, on their own pass.
//
// It never starts, stops, or removes a container: correcting the record
// is safe, while acting on a stale record is how a reconciler takes down
// a working app.
func Reconcile(ctx context.Context, repo *Repository, d reconcileDocker) error {
	here, err := repo.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	apps, err := repo.List(ctx)
	if err != nil {
		return err
	}

	for _, a := range apps {
		for _, mine := range a.ReplicasOn(here) {
			if mine.Container == "" {
				continue
			}
			running, err := d.IsRunning(ctx, mine.Container)
			if err != nil {
				log.Printf("reconcile: app %s: inspect container %s failed: %v", a.Name, mine.Container, err)
				running = false
			}

			want := StatusDown
			if running {
				want = StatusRunning
			}
			if want != mine.Status {
				log.Printf("reconcile: app %s copy %d: status %s -> %s", a.Name, mine.Ordinal, mine.Status, want)
				if err := repo.SetStatus(ctx, a.ID, here, mine.Ordinal, want); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// SettleInterrupted closes the deploys a restart of this daemon left
// with nobody working on them. It runs at startup, after Reconcile, and
// before anything can start a deploy.
//
// A deploy's control-plane half — resolving the image, which is where a
// build happens, and swapping the containers on this machine — runs on a
// goroutine of the process that started it. Replacing the daemon, an
// update most of all, kills that goroutine, and nothing afterwards comes
// for the row: it said `pending` for ever, and a deploy that has not
// finished cannot be deleted.
//
//   - No image yet: the build never finished, and nothing will resume
//     it. Failed.
//   - This machine runs the app and was not yet on this deploy: the swap
//     never happened. Failed — the containers are still the previous
//     deploy's, which is exactly what DeploymentToRun falls back to.
//   - Every copy already runs it: the swap finished and the row was not
//     closed. Succeeded.
//   - Otherwise it is waiting on other machines, which pick a pending
//     deploy up when they call in. Left alone — see Stall.
func SettleInterrupted(ctx context.Context, repo *Repository) error {
	here, err := repo.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	pending, err := repo.PendingSince(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, d := range pending {
		a, err := repo.ByID(ctx, d.AppID)
		if err != nil {
			log.Printf("settle interrupted deploy %d: read app %d: %v", d.ID, d.AppID, err)
			continue
		}
		status, why := interruptedOutcome(d, a, here)
		if status == "" {
			continue
		}
		log.Printf("settle interrupted deploy %d of app %s: %s", d.ID, a.Name, status)
		if err := repo.FinishDeployment(ctx, d.ID, status, why); err != nil {
			return err
		}
	}
	return nil
}

// interruptedOutcome is what a pending deploy found at startup ends as,
// or "" when it is still somebody's to finish.
func interruptedOutcome(d *Deployment, a *App, here int64) (status, why string) {
	if !resolvedImage(d.ImageRef) {
		return DeploymentFailed, "the daemon restarted before this deploy had an image; deploy again"
	}
	for _, r := range a.ReplicasOn(here) {
		if r.Deploy != d.ID {
			return DeploymentFailed, "the daemon restarted before this machine ran this deploy; deploy again"
		}
	}
	if len(a.Replicas) == 0 {
		return "", ""
	}
	for _, r := range a.Replicas {
		if r.Deploy != d.ID || !r.Running() {
			return "", ""
		}
	}
	return DeploymentSucceeded, ""
}

// resolvedImage tells an image reference from what a deploy was asked
// for. StartDeployment records the tag — `master`, `1.27`, or nothing —
// and SetDeploymentImage replaces it with a reference once the source
// has one, which always names a repository: a tag can hold neither a
// slash nor a colon.
func resolvedImage(ref string) bool {
	return strings.ContainsAny(ref, "/:@")
}
