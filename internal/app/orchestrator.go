package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/traefik"
	"cubeship/internal/project"
)

// DockerAPI is the subset of dockerx.Client the deploy engine needs.
// *dockerx.Client satisfies it structurally, and a test supplies a fake.
type DockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	IsRunning(ctx context.Context, id string) (bool, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
}

// Orchestrator runs deploys: it is the only thing in Cubeship that
// creates or retires an app's container.
type Orchestrator struct {
	db     *database.DB
	docker DockerAPI
	apps   *Repository
	proj   *project.Repository
	envs   *project.EnvironmentRepository

	// HealthCheckAttempts bounds how many observations waitHealthy takes
	// before giving up; HealthCheckSuccesses is how many of them must be
	// consecutively good for the container to count as healthy.
	HealthCheckAttempts  int
	HealthCheckSuccesses int
	HealthCheckInterval  time.Duration

	// appLocks serializes Deploy per app name. Without it two pushes in
	// quick succession both read the same app.ContainerID, both create a
	// container, and the loser's container is leaked while Traefik load
	// balances two versions under one router name.
	appLocks sync.Map // app name -> *sync.Mutex
}

func NewOrchestrator(db *database.DB, d DockerAPI) *Orchestrator {
	return &Orchestrator{
		db:                   db,
		docker:               d,
		apps:                 NewRepository(db),
		proj:                 project.NewRepository(db),
		envs:                 project.NewEnvironmentRepository(db),
		HealthCheckAttempts:  10,
		HealthCheckSuccesses: 3,
		HealthCheckInterval:  500 * time.Millisecond,
	}
}

// lockApp returns the mutex guarding deploys of the named app.
func (o *Orchestrator) lockApp(appName string) *sync.Mutex {
	mu, _ := o.appLocks.LoadOrStore(appName, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// Deploy pulls imageRef, starts a container from it, waits for it to look
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
func (o *Orchestrator) Deploy(ctx context.Context, appName, imageRef string) error {
	mu := o.lockApp(appName)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, appName)
	}

	env, err := o.inheritedEnv(ctx, a)
	if err != nil {
		return fmt.Errorf("resolve inherited env: %w", err)
	}

	if err := o.docker.PullImage(ctx, imageRef); err != nil {
		o.recordFailure(ctx, a.ID, imageRef, err.Error())
		return fmt.Errorf("pull image: %w", err)
	}

	newName := fmt.Sprintf("cubeship-%s-%d", appName, time.Now().UnixNano())
	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:    newName,
		Image:   imageRef,
		Labels:  traefik.Labels(appName, a.Domain, Port),
		Env:     envvar.Slice(env),
		Network: Network,
	})
	if err != nil {
		o.recordFailure(ctx, a.ID, imageRef, err.Error())
		return fmt.Errorf("create container: %w", err)
	}

	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that would not start")
		o.recordFailure(ctx, a.ID, imageRef, err.Error())
		return fmt.Errorf("start container: %w", err)
	}

	if !o.waitHealthy(ctx, newID) {
		o.removeContainer(ctx, newID, "abandoning a container that never became healthy")
		o.recordFailure(ctx, a.ID, imageRef, "health check timed out")
		return fmt.Errorf("health check timed out for container %s", newID)
	}

	oldContainerID := a.ContainerID
	if err := o.apps.UpdateContainer(ctx, a.ID, newID, StatusRunning); err != nil {
		// The new container is healthy but the database doesn't know
		// about it, so nothing will ever retire it. Remove it rather
		// than leave two containers answering one router.
		o.removeContainer(ctx, newID, "rolling back a deploy the database did not record")
		o.recordFailure(ctx, a.ID, imageRef, err.Error())
		return fmt.Errorf("update app container: %w", err)
	}
	if err := o.apps.RecordDeployment(ctx, a.ID, imageRef, "success", ""); err != nil {
		log.Printf("deploy %s: could not record the successful deployment: %v", appName, err)
	}

	if oldContainerID != "" {
		if err := o.docker.StopContainer(ctx, oldContainerID); err != nil {
			log.Printf("deploy %s: could not stop the previous container %s: %v", appName, oldContainerID, err)
		}
		o.removeContainer(ctx, oldContainerID, "retiring the previous container")
	}
	return nil
}

// recordFailure writes a failed deployment row. The deploy has already
// failed by the time this runs, so a further failure here is logged
// rather than returned — it must not mask the error the caller needs.
func (o *Orchestrator) recordFailure(ctx context.Context, appID int64, imageRef, msg string) {
	if err := o.apps.RecordDeployment(ctx, appID, imageRef, "failed", msg); err != nil {
		log.Printf("could not record the failed deployment of %s: %v", imageRef, err)
	}
}

// removeContainer removes a container that should no longer exist. A
// failure here leaks a container, which is worth a log line: nothing else
// will ever clean it up.
func (o *Orchestrator) removeContainer(ctx context.Context, id, why string) {
	if err := o.docker.RemoveContainer(ctx, id); err != nil {
		log.Printf("%s: could not remove container %s, it is now orphaned: %v", why, id, err)
	}
}

// Logs returns appName's container log. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (o *Orchestrator) Logs(ctx context.Context, appName, tail string) (io.ReadCloser, error) {
	a, err := o.apps.ByName(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, appName)
	}
	if a.ContainerID == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, a.ContainerID, tail)
}

// waitHealthy reports whether a freshly started container looks healthy.
//
// It requires HealthCheckSuccesses *consecutive* running observations,
// and waits HealthCheckInterval before each one — including the first.
// Checking immediately after ContainerStart proves nothing: Docker
// reports the container running the instant the process is spawned,
// before the app has had any chance to crash. And because every container
// carries RestartPolicy: unless-stopped, a crash-looping app
// intermittently reports running, so a single positive observation can be
// pure luck; requiring a run of them raises the bar without needing
// per-app health configuration.
//
// TODO (follow-up): an actual HTTP probe against Port would be a stronger
// signal than the container's process state.
func (o *Orchestrator) waitHealthy(ctx context.Context, containerID string) bool {
	needed := o.HealthCheckSuccesses
	if needed < 1 {
		needed = 1
	}

	consecutive := 0
	for i := 0; i < o.HealthCheckAttempts; i++ {
		if o.HealthCheckInterval > 0 {
			// Waiting on ctx as well as the timer: a cancelled deploy
			// must not keep sleeping through every remaining attempt.
			select {
			case <-ctx.Done():
				return false
			case <-time.After(o.HealthCheckInterval):
			}
		}
		running, err := o.docker.IsRunning(ctx, containerID)
		if err != nil || !running {
			consecutive = 0
			continue
		}
		consecutive++
		if consecutive >= needed {
			return true
		}
	}
	return false
}

// inheritedEnv resolves the full environment a deploy of a should run
// with: the project's vars, overridden by its environment's vars,
// overridden by the app's own vars — so an app can override a value its
// environment sets, and an environment can override one its project sets,
// but never the other way around.
func (o *Orchestrator) inheritedEnv(ctx context.Context, a *App) (envvar.Map, error) {
	p, err := o.proj.ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	e, err := o.envs.ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("get environment: %w", err)
	}
	return envvar.Merge(p.Env, e.Env, a.Env), nil
}
