package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/extregistry"
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/project"
	"cubeship/internal/settings"
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

	a, err := NewRepository(db).Create(ctx, o.ID, p.ID, env.ID, "myapp", "myapp.example.com", SourceRegistry, Origin{})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	cfg := settings.NewService(db)
	if err := cfg.SeedFromEnv(ctx, map[string]string{settings.Domain: "example.com"}); err != nil {
		t.Fatalf("configure the domain: %v", err)
	}
	orch := NewOrchestrator(db, docker, cfg, extregistry.NewService(db, nil), nil)
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

	if d := runDeploy(t, orch, db, a.ID, "v2"); d.Status != DeploymentSucceeded {
		t.Fatalf("deploy ended %q: %s", d.Status, d.Error)
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

	updated, err := NewRepository(db).ByID(ctx, a.ID)
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

	if d := runDeploy(t, orch, db, a.ID, "bad"); d.Status != DeploymentFailed {
		t.Fatal("expected the deploy to fail when the container never becomes healthy")
	}

	_, _, stopped, removed := docker.snapshot()
	if !slices.Contains(removed, "new-container") {
		t.Errorf("the unhealthy container was left behind: removed %v", removed)
	}
	if slices.Contains(stopped, "old-container") || slices.Contains(removed, "old-container") {
		t.Errorf("a failed deploy retired the working container: stopped %v, removed %v", stopped, removed)
	}

	unchanged, err := NewRepository(db).ByID(ctx, a.ID)
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
	orch, db, a := newDeployFixture(t, docker)
	orch.HealthCheckAttempts = 6
	orch.HealthCheckSuccesses = 3

	if d := runDeploy(t, orch, db, a.ID, "flappy"); d.Status != DeploymentFailed {
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

	if d := runDeploy(t, orch, db, a.ID, "v1"); d.Status != DeploymentSucceeded {
		t.Fatalf("deploy ended %q: %s", d.Status, d.Error)
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

// waitHealthy has to honour the deploy's own deadline. Nothing else
// stops a deploy whose container never comes up from holding its lock
// for the full run of attempts.
func TestDeployStopsWaitingWhenItsOwnContextEnds(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: false}
	orch, db, a := newDeployFixture(t, docker)
	orch.HealthCheckAttempts = 100
	orch.HealthCheckInterval = time.Hour

	deployment, err := NewRepository(db).StartDeployment(context.Background(), a.ID, "v1")
	if err != nil {
		t.Fatalf("record the deployment: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- orch.deploy(ctx, a.ID, "v1", deployment.ID) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a deploy with a dead context to fail")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the deploy kept waiting on the health check past its own deadline")
	}
}

// runDeploy starts a deploy and waits for it, returning the finished
// deployment — the shape every caller sees now that deploys are
// detached.
func runDeploy(t *testing.T, orch *Orchestrator, db *database.DB, appID int64, tag string) *Deployment {
	t.Helper()
	ctx := context.Background()

	deployment, err := orch.Start(ctx, appID, tag)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	finished, err := NewRepository(db).DeploymentByID(ctx, appID, deployment.ID)
	if err != nil {
		t.Fatalf("read the deployment back: %v", err)
	}
	return finished
}

// The fix for a deploy dying with the request that asked for it: Start
// returns at once, and the work carries on against a context of its own.
func TestStartDetachesTheDeployFromItsCaller(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newDeployFixture(t, docker)

	// A context that is already dead. If the deploy ran on it, nothing
	// would happen at all.
	dead, cancel := context.WithCancel(context.Background())
	deployment, err := orch.Start(dead, a.ID, "v1")
	cancel()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if deployment.Status != DeploymentPending {
		t.Errorf("a just-accepted deploy is %q, want %q", deployment.Status, DeploymentPending)
	}

	orch.Wait()

	finished, err := NewRepository(db).DeploymentByID(context.Background(), a.ID, deployment.ID)
	if err != nil {
		t.Fatalf("read the deployment back: %v", err)
	}
	if finished.Status != DeploymentSucceeded {
		t.Fatalf("deploy ended %q (%s), want it to have run to completion", finished.Status, finished.Error)
	}

	updated, err := NewRepository(db).ByID(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ContainerID != "new-container" {
		t.Errorf("the app records container %q; the detached deploy did not finish its work", updated.ContainerID)
	}
}

// A failed deploy has to say why, since nobody is holding a connection
// open to be told.
func TestFailedDeployRecordsWhy(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", pullErr: errors.New("manifest unknown")}
	orch, db, a := newDeployFixture(t, docker)

	deployment, err := orch.Start(context.Background(), a.ID, "nope")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	finished, err := NewRepository(db).DeploymentByID(context.Background(), a.ID, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != DeploymentFailed {
		t.Fatalf("deploy ended %q, want failed", finished.Status)
	}
	if !strings.Contains(finished.Error, "manifest unknown") {
		t.Errorf("the recorded error is %q; it should carry what actually went wrong", finished.Error)
	}
}

// Asking to deploy something that isn't there is the caller's error, and
// must surface as one rather than as a background failure they never see.
func TestStartRejectsAnUnknownApp(t *testing.T) {
	orch, _, _ := newDeployFixture(t, &fakeDocker{})

	if _, err := orch.Start(context.Background(), 9999, "v1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Start on an unknown app returned %v, want ErrNotFound", err)
	}
}

// WaitFor stops waiting when its context does, and says so — without
// touching the deploy.
func TestWaitForGivesUpOnItsContextNotOnTheDeploy(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newDeployFixture(t, docker)
	// Slow the health check enough that the wait below times out first.
	orch.HealthCheckInterval = 200 * time.Millisecond

	deployment, err := orch.Start(context.Background(), a.ID, "v1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	brief, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := orch.WaitFor(brief, a.ID, deployment.ID); err == nil {
		t.Fatal("expected the wait to time out")
	}

	// The deploy carried on regardless.
	orch.Wait()
	finished, err := NewRepository(db).DeploymentByID(context.Background(), a.ID, deployment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != DeploymentSucceeded {
		t.Errorf("abandoning the wait ended the deploy: %q (%s)", finished.Status, finished.Error)
	}
}
