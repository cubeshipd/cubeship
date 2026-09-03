package deploy

import (
	"context"
	"errors"
	"testing"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type fakeDocker struct {
	nextCreateID string
	running      bool
	pulledRef    string
	createdOpts  dockerx.ContainerOpts
	startedID    string
	stoppedIDs   []string
	removedIDs   []string
	createErr    error
	startErr     error
}

func (f *fakeDocker) PullImage(ctx context.Context, ref string) error {
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
	return f.running, nil
}

func newTestOrchestrator(t *testing.T, docker *fakeDocker) (*Orchestrator, *store.Store) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	o := New(s, docker)
	o.HealthCheckAttempts = 1
	o.HealthCheckInterval = 0
	return o, s
}

func TestDeploySuccessFirstDeploy(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

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

func TestDeploySwapsOldContainer(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true}
	o, s := newTestOrchestrator(t, docker)

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
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

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
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

func TestDeployUnknownApp(t *testing.T) {
	ctx := context.Background()
	o, _ := newTestOrchestrator(t, &fakeDocker{})

	err := o.Deploy(ctx, "does-not-exist", "registry.example.com/does-not-exist:latest")
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}
