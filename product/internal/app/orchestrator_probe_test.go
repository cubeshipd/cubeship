package app

import (
	"context"
	"testing"
)

type probeDocker struct {
	fakeDocker
	httpCalls []int
	tcpCalls  []int
}

func (d *probeDocker) ProbeHTTP(_ context.Context, _ string, _ string, port int) error {
	d.httpCalls = append(d.httpCalls, port)
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
	if !o.waitHealthy(context.Background(), "container", "/ready", healthPort(a), false) {
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
	if !o.waitHealthy(context.Background(), "container", "", healthPort(a), true) {
		t.Fatal("waitHealthy returned false")
	}
	if len(docker.tcpCalls) != 1 || docker.tcpCalls[0] != 7400 {
		t.Fatalf("TCP probes = %v, want [7400]", docker.tcpCalls)
	}
	if len(docker.httpCalls) != 0 {
		t.Fatalf("HTTP probes = %v, want none", docker.httpCalls)
	}
}
