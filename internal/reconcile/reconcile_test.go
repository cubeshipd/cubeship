package reconcile

import (
	"context"
	"testing"

	"cubeship/internal/store"
)

type fakeDocker struct {
	running map[string]bool
}

func (f *fakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	running, ok := f.running[id]
	if !ok {
		return false, nil
	}
	return running, nil
}

func TestReconcileMarksMissingContainerDown(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	docker := &fakeDocker{running: map[string]bool{}} // container-1 not found -> not running

	if err := Run(ctx, s, docker); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Status != "down" {
		t.Fatalf("expected status 'down', got %q", got.Status)
	}
	if got.ContainerID != "container-1" {
		t.Fatalf("expected container ID to be preserved for diagnosis, got %q", got.ContainerID)
	}
}

func TestReconcileConfirmsRunningContainer(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	app, _ := s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	docker := &fakeDocker{running: map[string]bool{"container-1": true}}

	if err := Run(ctx, s, docker); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Status != "running" {
		t.Fatalf("expected status to stay 'running', got %q", got.Status)
	}
}

func TestReconcileSkipsAppsNeverDeployed(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	// Should not panic or error even though there's no container to check.
	if err := Run(ctx, s, &fakeDocker{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
