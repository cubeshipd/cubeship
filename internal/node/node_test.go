package node

import (
	"testing"
	"time"
)

// A status is worked out from when the machine last called, on every
// read. Nothing writes it, and nothing runs on a timer to keep it true
// — which is the point: a column saying a machine is up is only as
// honest as whatever was supposed to update it, and the moment that
// misses a pass the table is lying about a box that is gone.
func TestAStatusIsWhenTheMachineLastCalled(t *testing.T) {
	recent := time.Now().Add(-Interval)
	stale := time.Now().Add(-Interval*MissedBeats - time.Second)

	cases := []struct {
		what string
		node Node
		want string
	}{
		{"the control plane is the process answering", Node{ControlPlane: true}, StatusReady},
		{"never connected", Node{}, StatusPending},
		{"called a moment ago", Node{LastSeenAt: &recent}, StatusReady},
		{"stopped calling", Node{LastSeenAt: &stale}, StatusUnreachable},
	}
	for _, c := range cases {
		if got := c.node.Status(); got != c.want {
			t.Errorf("%s: Status() = %q, want %q", c.what, got, c.want)
		}
	}
}

// A node that misses one pass is not a node that is gone. Three is
// what keeps a reconcile that ran long, or a daemon restarting after an
// upgrade, from flipping a healthy machine to a fault and back on the
// next screen refresh.
func TestOneMissedPassIsNotAFault(t *testing.T) {
	missedOne := time.Now().Add(-Interval * 2)
	n := Node{LastSeenAt: &missedOne}
	if got := n.Status(); got != StatusReady {
		t.Errorf("a node that missed a single pass reported %q", got)
	}
}

// The names the API's own paths take under /nodes cannot be a machine's
// name, for the reason `settings` cannot be an app's: Go's mux prefers
// a literal segment, so the node would be one nothing could open.
func TestANodeCannotTakeANameThePathsUse(t *testing.T) {
	for _, name := range []string{"agent", "settings", ControlPlaneSlug} {
		if err := checkSlug(name); err == nil {
			t.Errorf("checkSlug(%q) allowed a name the API answers at", name)
		}
	}
	for _, name := range []string{"eu-1", "worker-2", "hetzner-fsn1"} {
		if err := checkSlug(name); err != nil {
			t.Errorf("checkSlug(%q) = %v", name, err)
		}
	}
}
