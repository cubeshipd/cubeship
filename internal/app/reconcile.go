package app

import (
	"context"
	"log"
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
		mine, ok := a.ReplicaOn(here)
		if !ok || mine.Container == "" {
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
			log.Printf("reconcile: app %s: status %s -> %s", a.Name, mine.Status, want)
			if err := repo.SetStatus(ctx, a.ID, here, want); err != nil {
				return err
			}
		}
	}
	return nil
}
