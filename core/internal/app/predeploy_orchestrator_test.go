package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"cubeship/internal/envvar"
)

func TestPredeployRunsBeforeSwapWithEnvironmentAndNetwork(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true, oneShotOutput: "migration ok\n"}
	orch, db, a := newDeployFixture(t, docker)
	a.Predeploy = Predeploy{Shell: "./migrate", Timeout: time.Minute}
	if _, err := NewRepository(db).Update(context.Background(), a.ID, nil, nil, nil, nil, nil, &a.Predeploy); err != nil {
		t.Fatal(err)
	}
	if err := NewRepository(db).SetEnv(context.Background(), a.ID, envvar.Map{"SECRET": "private-value"}); err != nil {
		t.Fatal(err)
	}
	seedContainer(t, db, a.ID, "old-container")
	got := runDeploy(t, orch, db, a.ID, "v2")
	if got.Status != DeploymentSucceeded {
		t.Fatalf("status %s: %s", got.Status, got.Error)
	}
	docker.mu.Lock()
	opts := docker.oneShotOpts[0]
	stopped := append([]string(nil), docker.stoppedIDs...)
	docker.mu.Unlock()
	if opts.Image == "" || opts.Network != Network || opts.Entrypoint[0] != "sh" {
		t.Fatalf("unexpected hook options: %+v", opts)
	}
	if !strings.Contains(strings.Join(opts.Env, "\n"), "SECRET=private-value") {
		t.Fatal("hook did not receive app environment")
	}
	if len(stopped) == 0 || stopped[0] != "old-container" {
		t.Fatalf("swap did not happen after hook: %v", stopped)
	}
	if redactPredeployOutput("private-value", envvar.Map{"SECRET": "private-value"}) != "[REDACTED]" {
		t.Fatal("hook logs leaked environment secret")
	}
}

func TestPredeployFailureKeepsOldContainer(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true, oneShotCode: 1, oneShotOutput: "failed"}
	orch, db, a := newDeployFixture(t, docker)
	p := Predeploy{Args: []string{"migrate"}}
	if _, err := NewRepository(db).Update(context.Background(), a.ID, nil, nil, nil, nil, nil, &p); err != nil {
		t.Fatal(err)
	}
	seedContainer(t, db, a.ID, "old-container")
	got := runDeploy(t, orch, db, a.ID, "v2")
	if got.Status != DeploymentFailed || got.Error == "" {
		t.Fatalf("unexpected outcome: %+v", got)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("old container was stopped: %v", docker.stoppedIDs)
	}
}

func TestPredeployTimeoutIsCancellableAndMarked(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true, oneShotCode: -1}
	orch, db, a := newDeployFixture(t, docker)
	p := Predeploy{Args: []string{"wait"}, Timeout: time.Second}
	if _, err := NewRepository(db).Update(context.Background(), a.ID, nil, nil, nil, nil, nil, &p); err != nil {
		t.Fatal(err)
	}
	seedContainer(t, db, a.ID, "old-container")
	got := runDeploy(t, orch, db, a.ID, "v2")
	if got.Status != DeploymentFailed || !strings.Contains(got.Error, "timed out") {
		t.Fatalf("unexpected timeout: %+v", got)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("old container was stopped: %v", docker.stoppedIDs)
	}
}

func TestPredeployCancellationKeepsOldContainerAndIsObservable(t *testing.T) {
	docker := &fakeDocker{nextCreateID: "new-container", running: true, oneShotCode: -1}
	orch, db, a := newDeployFixture(t, docker)
	p := Predeploy{Args: []string{"wait"}, Timeout: time.Minute}
	if _, err := NewRepository(db).Update(context.Background(), a.ID, nil, nil, nil, nil, nil, &p); err != nil {
		t.Fatal(err)
	}
	seedContainer(t, db, a.ID, "old-container")
	d, err := orch.Start(context.Background(), a.ID, "v2")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if !orch.Cancel(d.ID) {
		t.Fatal("cancel did not find running deploy")
	}
	orch.Wait()
	got, err := NewRepository(db).DeploymentByID(context.Background(), a.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != DeploymentFailed || !got.Cancelled || got.Phase != DeploymentPhaseCancelled {
		t.Fatalf("unexpected cancellation: %+v", got)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("old container was stopped: %v", docker.stoppedIDs)
	}
}

func TestSameAppDeployLockQueuesSecondRun(t *testing.T) {
	docker := &fakeDocker{}
	orch, _, a := newDeployFixture(t, docker)
	mu := orch.lockApp(a.ID)
	mu.Lock()
	done := make(chan struct{})
	go func() { orch.lockApp(a.ID).Lock(); close(done) }()
	select {
	case <-done:
		t.Fatal("second deploy acquired the app lock concurrently")
	case <-time.After(20 * time.Millisecond):
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queued deploy did not acquire the lock")
	}
	orch.lockApp(a.ID).Unlock()
}
