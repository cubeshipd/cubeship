package app_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/metrics"
	"cubeship/internal/server/servertest"
)

// autoscaled turns the rule on for an app and hands back the fixture's
// autoscaler, which a test drives a pass at a time rather than waiting
// for a ticker.
func autoscaled(t *testing.T, f *servertest.Fixture, ref string, rule map[string]any) *app.Autoscaler {
	t.Helper()
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/apps/"+ref,
		map[string]any{"autoscale": rule}, f.AdminKey), http.StatusOK)
	return &app.Autoscaler{Apps: f.Server.Apps, Metrics: f.Server.Metrics}
}

// record writes readings for an app's copies, the way a machine's own
// pass does — the same table, the same kind, whichever machine took them.
func record(t *testing.T, f *servertest.Fixture, appID int64, cpu float64, n int) {
	t.Helper()
	ids := make([]int64, n)
	samples := make([]metrics.Sample, n)
	for i := range n {
		ids[i] = appID
		samples[i] = metrics.Sample{At: time.Now().Add(-time.Duration(i) * 30 * time.Second), CPUPercent: cpu}
	}
	if err := f.Server.Metrics.Record(context.Background(), metrics.KindApp, ids, samples); err != nil {
		t.Fatalf("record readings: %v", err)
	}
}

func appID(t *testing.T, f *servertest.Fixture, ref string) int64 {
	t.Helper()
	var id int64
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT a.id FROM apps a
		 JOIN projects p ON p.id = a.project_id
		 JOIN environments e ON e.id = a.environment_id
		 WHERE p.slug || '/' || e.slug || '/' || a.name = $1`, ref).Scan(&id); err != nil {
		t.Fatalf("find app %s: %v", ref, err)
	}
	return id
}

// **A busy app gets another copy without anybody asking.** The reading
// is the average across its copies, which is what its own chart is.
func TestABusyAppGetsAnotherCopy(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)

	scaler := autoscaled(t, f, created.Reference, map[string]any{"min": 1, "max": 4, "cpu": 50})
	record(t, f, appID(t, f, created.Reference), 100, 6)

	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()

	// One copy averaging 100% of a core against a target of 50 needs
	// two to sit at target.
	if got := replicaCount(t, f, created.Reference); got != 2 {
		t.Errorf("the app runs %d copies, want 2", got)
	}
}

// And a quiet one gives one back. It is the same lever a person pulls,
// so the copy stops now rather than at the next deploy.
func TestAQuietAppGivesACopyBack(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{"scale": 4})
	deploy(t, f, created.Reference)

	scaler := autoscaled(t, f, created.Reference, map[string]any{"min": 1, "max": 8, "cpu": 50})
	record(t, f, appID(t, f, created.Reference), 5, 6)
	before := len(docker.removedContainers())

	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()

	if got := replicaCount(t, f, created.Reference); got != 1 {
		t.Errorf("the app runs %d copies, want 1", got)
	}
	if len(docker.removedContainers()) == before {
		t.Error("the copies it gave back are still running")
	}
}

// **A window with almost nothing in it is waited out.** An app just
// deployed has one reading, and a decision from one reading is a
// decision from noise.
func TestItWaitsForEnoughToGoOn(t *testing.T) {
	f := servertest.NewWithDocker(t, &cappingDocker{})
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)

	scaler := autoscaled(t, f, created.Reference, map[string]any{"min": 1, "max": 4, "cpu": 50})
	record(t, f, appID(t, f, created.Reference), 400, 1)

	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()

	if got := replicaCount(t, f, created.Reference); got != 1 {
		t.Errorf("it acted on one reading and went to %d copies", got)
	}
}

// A second pass right after a change does nothing: a copy takes time to
// start and longer to take its share, and acting again before that is
// acting on a reading that does not include the last decision.
func TestItWaitsAfterActing(t *testing.T) {
	f := servertest.NewWithDocker(t, &cappingDocker{})
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)

	scaler := autoscaled(t, f, created.Reference, map[string]any{"min": 1, "max": 8, "cpu": 50})
	record(t, f, appID(t, f, created.Reference), 400, 6)

	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()
	after := replicaCount(t, f, created.Reference)

	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()
	if got := replicaCount(t, f, created.Reference); got != after {
		t.Errorf("a second pass moved it from %d to %d inside the cooldown", after, got)
	}
}

// An app nobody handed over is not touched, however busy it is.
func TestAnAppNobodyHandedOverIsLeftAlone(t *testing.T) {
	f := servertest.NewWithDocker(t, &cappingDocker{})
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)
	record(t, f, appID(t, f, created.Reference), 900, 6)

	scaler := &app.Autoscaler{Apps: f.Server.Apps, Metrics: f.Server.Metrics}
	scaler.Once(context.Background())
	f.Server.Apps.WaitForDeploys()

	if got := replicaCount(t, f, created.Reference); got != 1 {
		t.Errorf("an app with no rule went to %d copies", got)
	}
}

// A rule with no ceiling is the one that matters, and it is refused
// where the person who typed it is still watching.
func TestARuleWithNoCeilingIsRefused(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "api")

	for _, rule := range []map[string]any{
		{"min": 1, "max": 0, "cpu": 50},
		{"min": 0, "max": 4, "cpu": 50},
		{"min": 5, "max": 4, "cpu": 50},
		{"min": 1, "max": 4, "cpu": 0},
	} {
		rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
			map[string]any{"autoscale": rule}, f.AdminKey)
		if rule["max"] == 0 && rule["min"] == 1 {
			// max 0 is how it is turned off, and that is legal.
			servertest.RequireStatus(t, rec, http.StatusOK)
			continue
		}
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
	}
}
