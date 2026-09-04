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
// state and corrects apps.status to match reality. It runs at startup,
// when the database may describe a world from before a reboot.
//
// It never starts, stops, or removes a container: correcting the record
// is safe, while acting on a stale record is how a reconciler takes down
// a working app.
func Reconcile(ctx context.Context, repo *Repository, d reconcileDocker) error {
	apps, err := repo.List(ctx)
	if err != nil {
		return err
	}

	for _, a := range apps {
		if a.ContainerID == "" {
			continue
		}
		running, err := d.IsRunning(ctx, a.ContainerID)
		if err != nil {
			log.Printf("reconcile: app %s: inspect container %s failed: %v", a.Name, a.ContainerID, err)
			running = false
		}

		want := StatusDown
		if running {
			want = StatusRunning
		}
		if want != a.Status {
			log.Printf("reconcile: app %s: status %s -> %s", a.Name, a.Status, want)
			if err := repo.UpdateContainer(ctx, a.ID, a.ContainerID, want); err != nil {
				return err
			}
		}
	}
	return nil
}
