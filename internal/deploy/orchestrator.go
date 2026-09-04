package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
	"cubeship/internal/traefik"
)

// appPort is the port every app container is expected to listen on.
// A per-app configurable port is out of scope for this sub-project.
const appPort = 8080

// appNetwork is the Docker network app containers must join. Traefik
// resolves a container's backend IP on this network specifically (see
// the traefik.docker.network label in traefik.Labels), so a container
// left on the default bridge is invisible to the proxy and its domain
// serves 503.
const appNetwork = "cubeship"

var ErrAppNotFound = errors.New("app not found")
var ErrNoContainer = errors.New("app has no running container")

// dockerAPI is the subset of dockerx.Client this package needs.
// *dockerx.Client satisfies it structurally.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	IsRunning(ctx context.Context, id string) (bool, error)
	Logs(ctx context.Context, id string) (io.ReadCloser, error)
}

type Orchestrator struct {
	store  *store.Store
	docker dockerAPI

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

func New(s *store.Store, d dockerAPI) *Orchestrator {
	return &Orchestrator{
		store:                s,
		docker:               d,
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

func NewOrchestrator(s *store.Store, d *dockerx.Client) *Orchestrator {
	return New(s, d)
}

// Deploy pulls imageRef, starts a container from it, waits for it to
// look healthy, and only then retires the app's previous container.
//
// imageRef is the reference the daemon itself can pull — for apps in the
// embedded registry that is the loopback-published host
// (127.0.0.1:5000/<repo>:<tag>), not the public registry.<domain> name
// the user pushes to. Pulling the public name would hairpin out to the
// VPS's own public IP and require an ACME certificate to already exist,
// which the spec says must never block a deploy.
//
// Deploys of the same app are serialized; deploys of different apps run
// concurrently.
func (o *Orchestrator) Deploy(ctx context.Context, appName, imageRef string) error {
	mu := o.lockApp(appName)
	mu.Lock()
	defer mu.Unlock()

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
		Name:    newName,
		Image:   imageRef,
		Labels:  labels,
		Env:     envSlice(app.Env),
		Network: appNetwork,
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

func (o *Orchestrator) Logs(ctx context.Context, appName string) (io.ReadCloser, error) {
	app, err := o.store.GetAppByName(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}
	if app.ContainerID == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, app.ContainerID)
}

// waitHealthy reports whether a freshly started container looks healthy.
//
// It requires HealthCheckSuccesses *consecutive* running observations,
// and waits HealthCheckInterval before each one — including the first.
// Checking immediately after ContainerStart proves nothing: Docker
// reports the container running the instant the process is spawned,
// before the app has had any chance to crash. And because every
// container carries RestartPolicy: unless-stopped, a crash-looping app
// intermittently reports running, so a single positive observation can
// be pure luck; requiring a run of them raises the bar without needing
// per-app health configuration.
//
// TODO (follow-up): an actual HTTP probe against appPort would be a
// stronger signal than the container's process state.
func (o *Orchestrator) waitHealthy(ctx context.Context, containerID string) bool {
	needed := o.HealthCheckSuccesses
	if needed < 1 {
		needed = 1
	}

	consecutive := 0
	for i := 0; i < o.HealthCheckAttempts; i++ {
		if o.HealthCheckInterval > 0 {
			time.Sleep(o.HealthCheckInterval)
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
