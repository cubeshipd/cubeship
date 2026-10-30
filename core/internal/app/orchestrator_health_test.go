package app

import "testing"

func TestHealthPortPrefersTheRoutedHTTPPort(t *testing.T) {
	a := &Scoped{
		App: App{
			Domains:  []Domain{{Port: 9000}},
			TCPPorts: []TCPPort{{ContainerPort: 7000}},
		},
	}
	if got := healthPort(a); got != 9000 {
		t.Fatalf("health port = %d, want routed domain port 9000", got)
	}
}

func TestHealthCheckPortOverridesTheDomainPort(t *testing.T) {
	a := &Scoped{App: App{
		Domains:      []Domain{{Port: 9000}},
		HealthChecks: []HealthCheck{{Path: "/ready", Port: 4310}},
	}}
	if got := healthPort(a); got != 4310 {
		t.Fatalf("health port = %d, want configured port 4310", got)
	}
}

func TestAnUnspecifiedDomainPortUsesTheOnlyInternalPort(t *testing.T) {
	a := &App{InternalPorts: []int{4310}}
	if got := domainPort(a, Domain{}); got != 4310 {
		t.Fatalf("default domain port = %d, want the only internal port 4310", got)
	}
	if got := domainPort(a, Domain{Port: 9000}); got != 9000 {
		t.Fatalf("explicit domain port = %d, want 9000", got)
	}
	a.InternalPorts = []int{4310, 4320}
	if got := domainPort(a, Domain{}); got != DefaultPort {
		t.Fatalf("ambiguous domain port = %d, want fallback %d", got, DefaultPort)
	}
}
