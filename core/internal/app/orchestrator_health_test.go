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
