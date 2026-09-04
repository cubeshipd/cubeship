package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type fakeDocker struct {
	nextCreateID string
	running      bool
	// runningSeq, when non-empty, is consumed one entry per IsRunning
	// call (falling back to `running` once exhausted). It lets a test
	// script a flapping container.
	runningSeq  []bool
	isRunningAt []time.Duration
	clockStart  time.Time
	pulledRef   string
	createdOpts dockerx.ContainerOpts
	startedID   string
	stoppedIDs  []string
	removedIDs  []string
	pullErr     error
	createErr   error
	startErr    error
}

func (f *fakeDocker) PullImage(ctx context.Context, ref string) error {
	if f.pullErr != nil {
		return f.pullErr
	}
	f.pulledRef = ref
	return nil
}

func (f *fakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createdOpts = opts
	return f.nextCreateID, nil
}

func (f *fakeDocker) StartContainer(ctx context.Context, id string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.startedID = id
	return nil
}

func (f *fakeDocker) StopContainer(ctx context.Context, id string) error {
	f.stoppedIDs = append(f.stoppedIDs, id)
	return nil
}

func (f *fakeDocker) RemoveContainer(ctx context.Context, id string) error {
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *fakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	if !f.clockStart.IsZero() {
		f.isRunningAt = append(f.isRunningAt, time.Since(f.clockStart))
	}
	if len(f.runningSeq) > 0 {
		next := f.runningSeq[0]
		f.runningSeq = f.runningSeq[1:]
		return next, nil
	}
	return f.running, nil
}

func (f *fakeDocker) Logs(ctx context.Context, id, tail string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func newTestOrchestrator(t *testing.T, docker *fakeDocker) (*Orchestrator, *store.Store) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	o := New(s, docker)
	// Keep the real HealthCheckSuccesses (3): a container must be
	// observed running three times in a row, so the fixture needs at
	// least that many attempts. The interval is zeroed so tests don't
	// sleep.
	o.HealthCheckAttempts = 3
	o.HealthCheckInterval = 0
	return o, s
}

func TestDeploySuccessFirstDeploy(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if docker.pulledRef != "registry.example.com/myapp:latest" {
		t.Fatalf("expected pull of the new ref, got %q", docker.pulledRef)
	}
	if docker.createdOpts.Labels["traefik.enable"] != "true" {
		t.Fatalf("expected traefik labels to be set, got %v", docker.createdOpts.Labels)
	}
	if docker.startedID != "container-1" {
		t.Fatalf("expected the new container to be started, got %q", docker.startedID)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected no container stopped on first deploy, got %v", docker.stoppedIDs)
	}

	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" || app.Status != "running" {
		t.Fatalf("unexpected app state after deploy: %+v", app)
	}

	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "success" {
		t.Fatalf("expected one successful deployment record, got %+v", deps)
	}
}

func TestDeployAttachesContainerToCubeshipNetwork(t *testing.T) {
	// traefik.Labels tells Traefik to resolve the backend IP on the
	// "cubeship" network. A container left on the default bridge is
	// invisible to the proxy and its domain serves 503.
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	if err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if docker.createdOpts.Network != "cubeship" {
		t.Fatalf("expected the app container on the cubeship network, got %q", docker.createdOpts.Network)
	}
	if docker.createdOpts.Labels["traefik.docker.network"] != docker.createdOpts.Network {
		t.Fatalf("traefik.docker.network label (%q) must match the network the container joins (%q)",
			docker.createdOpts.Labels["traefik.docker.network"], docker.createdOpts.Network)
	}
}

func TestDeploySwapsOldContainer(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:v2"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(docker.stoppedIDs) != 1 || docker.stoppedIDs[0] != "container-1" {
		t.Fatalf("expected old container-1 to be stopped, got %v", docker.stoppedIDs)
	}
	if len(docker.removedIDs) != 1 || docker.removedIDs[0] != "container-1" {
		t.Fatalf("expected old container-1 to be removed, got %v", docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-2" {
		t.Fatalf("expected app to point at the new container, got %q", got.ContainerID)
	}
}

func TestDeployHealthCheckFailureLeavesOldContainerRunning(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: false}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:v2")
	if err == nil {
		t.Fatal("expected Deploy to return an error on failed health check")
	}

	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected the healthy old container to stay untouched, got stopped: %v", docker.stoppedIDs)
	}
	if len(docker.removedIDs) != 1 || docker.removedIDs[0] != "container-2" {
		t.Fatalf("expected the failed new container to be removed, got %v", docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected app to still point at the old container, got %q", got.ContainerID)
	}

	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "failed" {
		t.Fatalf("expected one failed deployment record, got %+v", deps)
	}
}

func TestDeployForwardsAppEnv(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.SetAppEnv(ctx, app.ID, map[string]string{"PORT": "8080"})

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	found := false
	for _, kv := range docker.createdOpts.Env {
		if kv == "PORT=8080" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PORT=8080 in container env, got %v", docker.createdOpts.Env)
	}
}

// An app's effective environment is its project's vars, overridden by
// its environment's vars, overridden by its own vars — never the other
// direction. TestDeployForwardsAppEnv above already covers an app's own
// vars reaching the container; this covers the inheritance and the
// precedence order between all three layers.
func TestDeployInheritsProjectAndEnvironmentEnv(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	project, env, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")
	s.SetProjectEnv(ctx, project.ID, map[string]string{
		"SHARED":       "from-project",
		"PROJECT_ONLY": "from-project",
	})
	s.SetEnvironmentEnv(ctx, env.ID, map[string]string{
		"SHARED":   "from-environment",
		"ENV_ONLY": "from-environment",
	})
	app, _ := s.CreateApp(ctx, org.ID, project.ID, env.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.SetAppEnv(ctx, app.ID, map[string]string{"SHARED": "from-app", "APP_ONLY": "from-app"})

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	got := map[string]string{}
	for _, kv := range docker.createdOpts.Env {
		parts := strings.SplitN(kv, "=", 2)
		got[parts[0]] = parts[1]
	}

	if got["SHARED"] != "from-app" {
		t.Fatalf("expected the app's own value to win for a key set at every layer, got %q", got["SHARED"])
	}
	if got["PROJECT_ONLY"] != "from-project" {
		t.Fatalf("expected the project's value to be inherited, got %q", got["PROJECT_ONLY"])
	}
	if got["ENV_ONLY"] != "from-environment" {
		t.Fatalf("expected the environment's value to be inherited, got %q", got["ENV_ONLY"])
	}
	if got["APP_ONLY"] != "from-app" {
		t.Fatalf("expected the app's own value to be present, got %q", got["APP_ONLY"])
	}
}

func TestDeployUnknownApp(t *testing.T) {
	ctx := context.Background()
	o, _ := newTestOrchestrator(t, &fakeDocker{})

	err := o.Deploy(ctx, "does-not-exist", "registry.example.com/does-not-exist:latest")
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}

// --- health check (finding 6) ---

func TestDeployRejectsSingleRunningObservation(t *testing.T) {
	// Docker reports a container running the instant ContainerStart
	// returns, before the app process has had a chance to crash. One
	// positive observation must not be enough.
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true}
	o, s := newTestOrchestrator(t, docker)
	o.HealthCheckAttempts = 1
	o.HealthCheckSuccesses = 3

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	if err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:v2"); err == nil {
		t.Fatal("expected the deploy to fail: one running observation is not a health check")
	}
	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected the old container to still be live, got %q", got.ContainerID)
	}
}

func TestDeployRejectsFlappingContainer(t *testing.T) {
	// A crash-looping app under RestartPolicy: unless-stopped reports
	// running intermittently. Successes must be consecutive.
	ctx := context.Background()
	docker := &fakeDocker{
		nextCreateID: "container-2",
		runningSeq:   []bool{true, false, true, true, false, true},
		running:      false,
	}
	o, s := newTestOrchestrator(t, docker)
	o.HealthCheckAttempts = 6
	o.HealthCheckSuccesses = 3

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	if err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:v2"); err == nil {
		t.Fatal("expected a flapping container to fail its health check")
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected the healthy old container to stay untouched, got stopped: %v", docker.stoppedIDs)
	}
}

func TestDeployAcceptsConsecutiveRunningObservations(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{
		nextCreateID: "container-1",
		runningSeq:   []bool{false, true, true, true},
		running:      false,
	}
	o, s := newTestOrchestrator(t, docker)
	o.HealthCheckAttempts = 4
	o.HealthCheckSuccesses = 3

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	if err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:latest"); err != nil {
		t.Fatalf("expected a container that settles to running to pass: %v", err)
	}
}

func TestDeployWaitsBeforeTheFirstHealthObservation(t *testing.T) {
	ctx := context.Background()
	const interval = 20 * time.Millisecond
	docker := &fakeDocker{nextCreateID: "container-1", running: true, clockStart: time.Now()}
	o, s := newTestOrchestrator(t, docker)
	o.HealthCheckAttempts = 3
	o.HealthCheckSuccesses = 3
	o.HealthCheckInterval = interval

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	if err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(docker.isRunningAt) == 0 {
		t.Fatal("expected at least one health observation")
	}
	if docker.isRunningAt[0] < interval {
		t.Fatalf("expected the first health check to wait one interval (%v) after start, it ran at %v",
			interval, docker.isRunningAt[0])
	}
}

// --- failure branches (finding 15) ---

func TestDeployPullFailure(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true, pullErr: errors.New("manifest unknown")}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:v2")
	if err == nil {
		t.Fatal("expected Deploy to fail when the pull fails")
	}

	if docker.createdOpts.Name != "" {
		t.Fatalf("expected no container to be created after a failed pull, got %q", docker.createdOpts.Name)
	}
	if len(docker.stoppedIDs) != 0 || len(docker.removedIDs) != 0 {
		t.Fatalf("expected the old container untouched, stopped=%v removed=%v", docker.stoppedIDs, docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected the app to still point at the old container, got %q", got.ContainerID)
	}
	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "failed" {
		t.Fatalf("expected one failed deployment record, got %+v", deps)
	}
	if !strings.Contains(deps[0].Error, "manifest unknown") {
		t.Fatalf("expected the pull error to be recorded, got %q", deps[0].Error)
	}
}

func TestDeployCreateFailure(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true, createErr: errors.New("no such image")}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:v2")
	if err == nil {
		t.Fatal("expected Deploy to fail when create fails")
	}

	if docker.startedID != "" {
		t.Fatalf("expected no start after a failed create, got %q", docker.startedID)
	}
	if len(docker.stoppedIDs) != 0 || len(docker.removedIDs) != 0 {
		t.Fatalf("expected the old container untouched, stopped=%v removed=%v", docker.stoppedIDs, docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected the app to still point at the old container, got %q", got.ContainerID)
	}
	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "failed" {
		t.Fatalf("expected one failed deployment record, got %+v", deps)
	}
}

func TestDeployStartFailure(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true, startErr: errors.New("port is already allocated")}
	o, s := newTestOrchestrator(t, docker)

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	err := o.Deploy(ctx, "myapp", "127.0.0.1:5000/myapp:v2")
	if err == nil {
		t.Fatal("expected Deploy to fail when start fails")
	}

	// The half-created container is cleaned up; the old one is not.
	if len(docker.removedIDs) != 1 || docker.removedIDs[0] != "container-2" {
		t.Fatalf("expected only the failed new container to be removed, got %v", docker.removedIDs)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected the healthy old container to stay untouched, got stopped: %v", docker.stoppedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected the app to still point at the old container, got %q", got.ContainerID)
	}
	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "failed" {
		t.Fatalf("expected one failed deployment record, got %+v", deps)
	}
}

// --- per-app serialization (finding 5) ---

// serialConcurrencyDocker measures how many deploys are inside the
// pull-to-start window at once. PullImage and StartContainer bracket
// that window and both run exactly once per successful deploy. It is
// safe for concurrent use.
type serialConcurrencyDocker struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	createSeq   int
	stoppedIDs  []string
	removedIDs  []string
}

func (f *serialConcurrencyDocker) PullImage(ctx context.Context, ref string) error {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()
	// Give a competing deploy a real chance to interleave.
	time.Sleep(20 * time.Millisecond)
	return nil
}

func (f *serialConcurrencyDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createSeq++
	return fmt.Sprintf("container-%d", f.createSeq), nil
}

func (f *serialConcurrencyDocker) StartContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()
	return nil
}

func (f *serialConcurrencyDocker) StopContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stoppedIDs = append(f.stoppedIDs, id)
	return nil
}

func (f *serialConcurrencyDocker) RemoveContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *serialConcurrencyDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return true, nil
}

func (f *serialConcurrencyDocker) Logs(ctx context.Context, id, tail string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *serialConcurrencyDocker) peak() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxInFlight
}

func (f *serialConcurrencyDocker) stoppedAndRemoved() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stoppedIDs...), append([]string(nil), f.removedIDs...)
}

func TestConcurrentDeploysOfSameAppAreSerialized(t *testing.T) {
	ctx := context.Background()
	docker := &serialConcurrencyDocker{}
	// A file-backed DB, not ":memory:": database/sql pools connections
	// and each ":memory:" connection would open its own database.
	s, err := store.Open(filepath.Join(t.TempDir(), "cubeship.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	o := New(s, docker)
	o.HealthCheckAttempts = 3
	o.HealthCheckInterval = 0

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = o.Deploy(ctx, "myapp", fmt.Sprintf("127.0.0.1:5000/myapp:v%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("deploy %d: %v", i, err)
		}
	}

	if got := docker.peak(); got != 1 {
		t.Fatalf("expected deploys of one app to be serialized, saw %d running concurrently", got)
	}

	// The second deploy must see the first's container as the one to
	// retire — nothing is leaked.
	app, _ := s.GetAppByName(ctx, "myapp")
	stopped, removed := docker.stoppedAndRemoved()
	if len(stopped) != 1 || len(removed) != 1 {
		t.Fatalf("expected exactly one container to be retired, stopped=%v removed=%v", stopped, removed)
	}
	if stopped[0] == app.ContainerID {
		t.Fatalf("the live container %q must not have been stopped", app.ContainerID)
	}
}

func TestConcurrentDeploysOfDifferentAppsRunInParallel(t *testing.T) {
	ctx := context.Background()
	docker := &serialConcurrencyDocker{}
	// A file-backed DB, not ":memory:": database/sql pools connections
	// and each ":memory:" connection would open its own database.
	s, err := store.Open(filepath.Join(t.TempDir(), "cubeship.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	o := New(s, docker)
	o.HealthCheckAttempts = 3
	o.HealthCheckInterval = 0

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "one", "one.example.com", "registry.example.com/one")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "two", "two.example.com", "registry.example.com/two")

	var wg sync.WaitGroup
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			o.Deploy(ctx, name, "127.0.0.1:5000/"+name+":latest")
		}(name)
	}
	wg.Wait()

	if got := docker.peak(); got != 2 {
		t.Fatalf("expected different apps to deploy concurrently, peak concurrency was %d", got)
	}
}

func TestLogsReturnsErrNoContainerBeforeFirstDeploy(t *testing.T) {
	ctx := context.Background()
	o, s := newTestOrchestrator(t, &fakeDocker{})
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	_, err := o.Logs(ctx, "myapp", "")
	if !errors.Is(err, ErrNoContainer) {
		t.Fatalf("expected ErrNoContainer, got %v", err)
	}
}
