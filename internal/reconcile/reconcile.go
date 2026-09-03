package reconcile

import (
	"context"
	"log"

	"cubeship/internal/store"
)

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	IsRunning(ctx context.Context, id string) (bool, error)
}

// Run compares each app's recorded container against real Docker state
// and corrects apps.status to match reality. It never starts, stops,
// or removes a container.
func Run(ctx context.Context, s *store.Store, d dockerAPI) error {
	apps, err := s.ListApps(ctx)
	if err != nil {
		return err
	}

	for _, app := range apps {
		if app.ContainerID == "" {
			continue
		}
		running, err := d.IsRunning(ctx, app.ContainerID)
		if err != nil {
			log.Printf("reconcile: app %s: inspect container %s failed: %v", app.Name, app.ContainerID, err)
			running = false
		}

		wantStatus := "down"
		if running {
			wantStatus = "running"
		}
		if wantStatus != app.Status {
			log.Printf("reconcile: app %s: status %s -> %s", app.Name, app.Status, wantStatus)
			if err := s.UpdateAppContainer(ctx, app.ID, app.ContainerID, wantStatus); err != nil {
				return err
			}
		}
	}
	return nil
}
