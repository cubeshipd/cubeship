package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/project"
	"cubeship/internal/user"
)

// newDeployFixture returns an orchestrator over a real database with one
// app already registered, plus the pieces a test needs to vary its
// environment.
func newDeployFixture(t *testing.T, docker DockerAPI) (*Orchestrator, *database.DB, *App) {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)

	users := user.NewService(db)
	orgs := org.NewService(db, users)
	projects := project.NewService(db, orgs)

	admin, err := user.NewRepository(db).Create(ctx, "admin", true)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	o, err := orgs.Repo().Create(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	p, env, err := projects.Create(ctx, admin, o.Slug, "web", "Web")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	a, err := NewRepository(db).Create(ctx, o.ID, p.ID, env.ID,
		"myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	orch := NewOrchestrator(db, docker)
	// The health check's real timing has nothing to test here, and
	// sleeping through it would make every deploy test slow.
	orch.HealthCheckInterval = 0
	return orch, db, a
}

// A deploy must never take the old container down before the new one is
// healthy — that ordering is the entire point of the orchestrator.
func TestDeployRetiresTheOldContainerOnlyAfterTheNewOneIsHealthy(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newDeployFixture(t, docker)
	ctx := context.Background()

	if err := NewRepository(db).UpdateContainer(ctx, a.ID, "old-container", StatusRunning); err != nil {
		t.Fatalf("seed the previous container: %v", err)
	}

	if err := orch.Deploy(ctx, a.Name, "127.0.0.1:5000/acme/myapp:v2"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	_, started, stopped, removed := docker.snapshot()
	if !slices.Contains(started, "new-container") {
		t.Fatalf("the new container was never started: %v", started)
	}
	if !slices.Contains(stopped, "old-container") || !slices.Contains(removed, "old-container") {
		t.Errorf("the old container was not retired: stopped %v, removed %v", stopped, removed)
	}
	if slices.Contains(removed, "new-container") {
		t.Error("the new container was removed by its own successful deploy")
	}

	updated, err := NewRepository(db).ByName(ctx, a.Name)
	if err != nil {
		t.Fatalf("reload app: %v", err)
	}
	if updated.ContainerID != "new-container" || updated.Status != StatusRunning {
		t.Errorf("app records container %q status %q, want new-container/running",
			updated.ContainerID, updated.Status)
	}
}

// A container that never becomes healthy must be discarded and the app
// left exactly as it was — a bad image must not take down a working app.
func TestDeployKeepsTheOldContainerWhenTheNewOneNeverBecomesHealthy(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: false}
	orch, db, a := newDeployFixture(t, docker)
	orch.HealthCheckAttempts = 3
	ctx := context.Background()

	if err := NewRepository(db).UpdateContainer(ctx, a.ID, "old-container", StatusRunning); err != nil {
		t.Fatalf("seed the previous container: %v", err)
	}

	if err := orch.Deploy(ctx, a.Name, "127.0.0.1:5000/acme/myapp:bad"); err == nil {
		t.Fatal("expected the deploy to fail when the container never becomes healthy")
	}

	_, _, stopped, removed := docker.snapshot()
	if !slices.Contains(removed, "new-container") {
		t.Errorf("the unhealthy container was left behind: removed %v", removed)
	}
	if slices.Contains(stopped, "old-container") || slices.Contains(removed, "old-container") {
		t.Errorf("a failed deploy retired the working container: stopped %v, removed %v", stopped, removed)
	}

	unchanged, err := NewRepository(db).ByName(ctx, a.Name)
	if err != nil {
		t.Fatalf("reload app: %v", err)
	}
	if unchanged.ContainerID != "old-container" {
		t.Errorf("app now records container %q, want old-container", unchanged.ContainerID)
	}
}

// A single lucky observation must not count as healthy: a crash-looping
// container reports running intermittently.
func TestDeployRequiresConsecutiveHealthyObservations(t *testing.T) {
	docker := &fakeDocker{
		nextCreateID: "new-container",
		// Up, down, up, down... never three in a row.
		runningSeq: []bool{true, false, true, false, true, false},
		running:    false,
	}
	orch, _, a := newDeployFixture(t, docker)
	orch.HealthCheckAttempts = 6
	orch.HealthCheckSuccesses = 3

	if err := orch.Deploy(context.Background(), a.Name, "127.0.0.1:5000/acme/myapp:flappy"); err == nil {
		t.Fatal("expected a flapping container to fail the health check")
	}
}

// The whole point of three-level variables: an app overrides its
// environment, which overrides its project.
func TestDeployAppliesInheritedEnvInPrecedenceOrder(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newDeployFixture(t, docker)
	ctx := context.Background()

	if err := project.NewRepository(db).SetEnv(ctx, a.ProjectID, envvar.Map{
		"SHARED": "from-project", "ONLY_PROJECT": "p",
	}); err != nil {
		t.Fatalf("set project env: %v", err)
	}
	if err := project.NewEnvironmentRepository(db).SetEnv(ctx, a.EnvironmentID, envvar.Map{
		"SHARED": "from-environment", "ONLY_ENV": "e",
	}); err != nil {
		t.Fatalf("set environment env: %v", err)
	}
	if err := NewRepository(db).SetEnv(ctx, a.ID, envvar.Map{"SHARED": "from-app"}); err != nil {
		t.Fatalf("set app env: %v", err)
	}

	if err := orch.Deploy(ctx, a.Name, "127.0.0.1:5000/acme/myapp:v1"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	created, _, _, _ := docker.snapshot()
	if len(created) != 1 {
		t.Fatalf("expected one container to be created, got %d", len(created))
	}
	env := created[0].Env
	for _, want := range []string{"SHARED=from-app", "ONLY_PROJECT=p", "ONLY_ENV=e"} {
		if !slices.Contains(env, want) {
			t.Errorf("container env %v is missing %q", env, want)
		}
	}
	if slices.Contains(env, "SHARED=from-project") || slices.Contains(env, "SHARED=from-environment") {
		t.Errorf("a lower level failed to override SHARED: %v", env)
	}
}

// A cancelled deploy must stop waiting rather than sleep through every
// remaining health-check attempt.
func TestDeployStopsWaitingWhenTheContextIsCancelled(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: false}
	orch, _, a := newDeployFixture(t, docker)
	orch.HealthCheckAttempts = 100
	orch.HealthCheckInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- orch.Deploy(ctx, a.Name, "127.0.0.1:5000/acme/myapp:v1") }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a cancelled deploy to fail")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled deploy kept waiting on the health check")
	}
}
