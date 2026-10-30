package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNetworkingValidatesPortsAndHealthCheckReferences(t *testing.T) {
	checks, err := validateNetworking([]int{4310}, []HealthCheck{{Path: "/ready", Port: 4310}})
	if err != nil {
		t.Fatal(err)
	}
	want := HealthCheck{Path: "/ready", Port: 4310, PeriodSeconds: 10, TimeoutSeconds: 5, FailureThreshold: 3, SuccessThreshold: 1}
	if !reflect.DeepEqual(checks, []HealthCheck{want}) {
		t.Fatalf("normalized checks = %#v, want %#v", checks, []HealthCheck{want})
	}
	if _, err := validateNetworking([]int{4310}, []HealthCheck{{Path: "/ready", Port: 8080}}); !errors.Is(err, ErrInvalidNetworking) {
		t.Fatalf("undeclared health port error = %v", err)
	}
	if _, err := validateNetworking([]int{4310, 4310}, nil); !errors.Is(err, ErrInvalidNetworking) {
		t.Fatalf("duplicate internal port error = %v", err)
	}
	if _, err := validateNetworking([]int{4310}, []HealthCheck{{
		Path: "/ready", Port: 4310, PeriodSeconds: 5, TimeoutSeconds: 5,
	}}); !errors.Is(err, ErrInvalidNetworking) {
		t.Fatalf("timeout equal to period error = %v", err)
	}
}

func TestEveryConfiguredHealthCheckMustPassForReadiness(t *testing.T) {
	docker := &probeDocker{fakeDocker: fakeDocker{running: true}}
	o := &Orchestrator{docker: docker, HealthCheckAttempts: 2}
	a := &Scoped{App: App{HealthChecks: []HealthCheck{
		{Path: "/ready", Port: 4310, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 2, SuccessThreshold: 1},
		{Path: "/metrics/ready", Port: 4320, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 2, SuccessThreshold: 1},
	}}}
	if err := o.waitAppHealthy(context.Background(), "container", a); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(docker.httpCalls, []int{4310, 4320}) {
		t.Fatalf("health probes used ports %v, want both configured ports", docker.httpCalls)
	}

	docker = &probeDocker{fakeDocker: fakeDocker{running: true}, failures: 2}
	o.docker = docker
	if err := o.waitAppHealthy(context.Background(), "container", a); err == nil {
		t.Fatal("readiness succeeded although the first health check failed")
	}
}
