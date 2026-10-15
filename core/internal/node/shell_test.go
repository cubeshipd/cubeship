package node

import (
	"errors"
	"testing"
	"time"
)

func TestAShellOpensOnlyOnAMachineThatCanTakeOne(t *testing.T) {
	now := time.Now()
	stale := now.Add(-time.Hour)
	for _, c := range []struct {
		name string
		n    Node
		want error
	}{
		{"the control plane", Node{ControlPlane: true}, nil},
		{"a current worker", Node{Version: "0.10.0", LastSeenAt: &now}, nil},
		{"a candidate of the release that added shells", Node{Version: "0.10.0-rc.1", LastSeenAt: &now}, nil},
		{"a development build", Node{Version: "dev", LastSeenAt: &now}, nil},
		{"a worker from before shells", Node{Version: "0.9.3", LastSeenAt: &now}, ErrTooOld},
		{"a worker that stopped calling", Node{Version: "0.10.0", LastSeenAt: &stale}, ErrOffline},
		{"a worker that never called", Node{Version: "0.10.0"}, ErrOffline},
	} {
		if err := c.n.CanOpenShell(); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}
