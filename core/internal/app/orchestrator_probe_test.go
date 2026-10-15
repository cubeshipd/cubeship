package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

type probeDocker struct {
	fakeDocker
	httpCalls []int
	tcpCalls  []int
	failures  int
}

func (d *probeDocker) ProbeHTTP(_ context.Context, _ string, _ string, port int) error {
	d.httpCalls = append(d.httpCalls, port)
	if len(d.httpCalls) <= d.failures {
		return errors.New("connection refused")
	}
	return nil
}

func (d *probeDocker) ProbeTCP(_ context.Context, _ string, port int) error {
	d.tcpCalls = append(d.tcpCalls, port)
	return nil
}

func TestWaitHealthyUsesHTTPProbeForHealthPath(t *testing.T) {
	docker := &probeDocker{fakeDocker: fakeDocker{running: true}}
	o := &Orchestrator{
		docker:               docker,
		HealthCheckAttempts:  1,
		HealthCheckSuccesses: 1,
	}
	a := &Scoped{App: App{Domains: []Domain{{Port: 9100}}}}
	if err := o.waitHealthy(context.Background(), "container", "/ready", healthPort(a), false); err != nil {
		t.Fatal("waitHealthy returned false")
	}
	if len(docker.httpCalls) != 1 || docker.httpCalls[0] != 9100 {
		t.Fatalf("HTTP probes = %v, want [9100]", docker.httpCalls)
	}
	if len(docker.tcpCalls) != 0 {
		t.Fatalf("TCP probes = %v, want none", docker.tcpCalls)
	}
}

func TestWaitHealthyUsesTCPProbeForPublishedPortWithoutHealthPath(t *testing.T) {
	docker := &probeDocker{fakeDocker: fakeDocker{running: true}}
	o := &Orchestrator{
		docker:               docker,
		HealthCheckAttempts:  1,
		HealthCheckSuccesses: 1,
	}
	a := &Scoped{App: App{TCPPorts: []TCPPort{{ContainerPort: 7400, HostPort: 17400}}}}
	if err := o.waitHealthy(context.Background(), "container", "", healthPort(a), true); err != nil {
		t.Fatal("waitHealthy returned false")
	}
	if len(docker.tcpCalls) != 1 || docker.tcpCalls[0] != 7400 {
		t.Fatalf("TCP probes = %v, want [7400]", docker.tcpCalls)
	}
	if len(docker.httpCalls) != 0 {
		t.Fatalf("HTTP probes = %v, want none", docker.httpCalls)
	}
}

func TestWaitHealthyKeepsLastProbeError(t *testing.T) {
	d := &probeDocker{fakeDocker: fakeDocker{running: true}, failures: 10}
	o := &Orchestrator{docker: d, HealthCheckAttempts: 2, HealthCheckSuccesses: 1}
	err := o.waitHealthy(context.Background(), "candidate", "/ready", 8080, false)
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("lost last probe error: %v", err)
	}
}

func TestWaitHealthyRetriesUntilConsecutiveSuccesses(t *testing.T) {
	d := &probeDocker{fakeDocker: fakeDocker{running: true}, failures: 2}
	o := &Orchestrator{docker: d, HealthCheckAttempts: 5, HealthCheckSuccesses: 2}
	if err := o.waitHealthy(context.Background(), "candidate", "/ready", 8080, false); err != nil {
		t.Fatal(err)
	}
	if len(d.httpCalls) != 4 {
		t.Fatalf("probe calls = %d", len(d.httpCalls))
	}
}

func TestWaitHealthyReturnsCancellation(t *testing.T) {
	d := &probeDocker{fakeDocker: fakeDocker{running: true}}
	o := &Orchestrator{docker: d, HealthCheckAttempts: 2}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.waitHealthy(ctx, "candidate", "/ready", 8080, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if len(d.httpCalls) != 0 {
		t.Fatal("probed after cancellation")
	}
}

func TestFailedProbeKeepsServingContainer(t *testing.T) {
	d := &probeDocker{fakeDocker: fakeDocker{nextCreateID: "candidate", running: true}, failures: 100}
	o, db, a := newDeployFixture(t, d)
	seedContainer(t, db, a.ID, "serving")
	if _, err := db.ExecContext(context.Background(), "UPDATE apps SET health_path = '/ready' WHERE id = $1", a.ID); err != nil {
		t.Fatal(err)
	}
	result := runDeploy(t, o, db, a.ID, "v2")
	if result.Status != DeploymentFailed || !strings.Contains(result.Error, "connection refused") {
		t.Fatalf("deployment = %#v", result)
	}
	_, _, stopped, removed := d.snapshot()
	if slices.Contains(stopped, "serving") || slices.Contains(removed, "serving") {
		t.Fatalf("serving container changed: stopped %v, removed %v", stopped, removed)
	}
	if !slices.Contains(removed, "candidate") {
		t.Fatalf("candidate leaked: %v", removed)
	}
}

type cancelProbeDocker struct {
	fakeDocker
	cancel context.CancelFunc
}

func (d *cancelProbeDocker) ProbeHTTP(ctx context.Context, _, _ string, _ int) error {
	d.cancel()
	return ctx.Err()
}
func (d *cancelProbeDocker) RemoveContainer(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fakeDocker.RemoveContainer(ctx, id)
}
func (d *cancelProbeDocker) StartContainer(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.fakeDocker.StartContainer(ctx, id)
}

func TestCanceledProbeCleansCandidateAndRestoresInPlace(t *testing.T) {
	for _, inPlace := range []bool{false, true} {
		t.Run(fmt.Sprint(inPlace), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := &cancelProbeDocker{fakeDocker: fakeDocker{nextCreateID: "candidate", running: true}, cancel: cancel}
			o, _, a := newDeployFixture(t, d)
			scoped := &Scoped{App: *a}
			scoped.HealthPath = "/ready"
			replica := Replica{Container: "serving"}
			var err error
			if inPlace {
				err = o.swapInPlace(ctx, scoped, replica, Image{Ref: "image"}, nil, nil, "test", 1)
			} else {
				err = o.swap(ctx, scoped, replica, Image{Ref: "image"}, nil, nil, "test", 1)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v", err)
			}
			_, started, stopped, removed := d.snapshot()
			if !slices.Contains(removed, "candidate") {
				t.Fatalf("candidate leaked: %v", removed)
			}
			if slices.Contains(removed, "serving") {
				t.Fatal("serving container removed")
			}
			if inPlace && !slices.Contains(started, "serving") {
				t.Fatal("previous serving version not restarted")
			}
			if !inPlace && slices.Contains(stopped, "serving") {
				t.Fatal("rolling deploy stopped serving version")
			}
		})
	}
}
