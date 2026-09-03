package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
	"cubeship/internal/traefik"
)

// appPort is the port every app container is expected to listen on.
// A per-app configurable port is out of scope for this sub-project.
const appPort = 8080

var ErrAppNotFound = errors.New("app not found")

// dockerAPI is the subset of dockerx.Client this package needs.
// *dockerx.Client satisfies it structurally.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	IsRunning(ctx context.Context, id string) (bool, error)
}

type Orchestrator struct {
	store  *store.Store
	docker dockerAPI

	HealthCheckAttempts int
	HealthCheckInterval time.Duration
}

func New(s *store.Store, d dockerAPI) *Orchestrator {
	return &Orchestrator{
		store:               s,
		docker:              d,
		HealthCheckAttempts: 10,
		HealthCheckInterval: 500 * time.Millisecond,
	}
}

func NewOrchestrator(s *store.Store, d *dockerx.Client) *Orchestrator {
	return New(s, d)
}

func (o *Orchestrator) Deploy(ctx context.Context, appName, imageRef string) error {
	app, err := o.store.GetAppByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}

	if err := o.docker.PullImage(ctx, imageRef); err != nil {
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("pull image: %w", err)
	}

	newName := fmt.Sprintf("cubeship-%s-%d", appName, time.Now().UnixNano())
	labels := traefik.Labels(appName, app.Domain, appPort)

	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:   newName,
		Image:  imageRef,
		Labels: labels,
		Env:    envSlice(app.Env),
	})
	if err != nil {
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("create container: %w", err)
	}

	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.docker.RemoveContainer(ctx, newID)
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("start container: %w", err)
	}

	if !o.waitHealthy(ctx, newID) {
		o.docker.RemoveContainer(ctx, newID)
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", "health check timed out")
		return fmt.Errorf("health check timed out for container %s", newID)
	}

	oldContainerID := app.ContainerID
	if err := o.store.UpdateAppContainer(ctx, app.ID, newID, "running"); err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	o.store.RecordDeployment(ctx, app.ID, imageRef, "success", "")

	if oldContainerID != "" {
		o.docker.StopContainer(ctx, oldContainerID)
		o.docker.RemoveContainer(ctx, oldContainerID)
	}

	return nil
}

func (o *Orchestrator) waitHealthy(ctx context.Context, containerID string) bool {
	for i := 0; i < o.HealthCheckAttempts; i++ {
		running, err := o.docker.IsRunning(ctx, containerID)
		if err == nil && running {
			return true
		}
		if o.HealthCheckInterval > 0 {
			time.Sleep(o.HealthCheckInterval)
		}
	}
	return false
}

func envSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
