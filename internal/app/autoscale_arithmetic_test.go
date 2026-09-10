package app

import (
	"testing"
	"time"
)

// The arithmetic, on its own: it is the whole of the decision, and
// every one of these is a way a rule stops settling.
func TestWhatTheRuleAsksFor(t *testing.T) {
	rule := Autoscale{Min: 2, Max: 8, CPU: 50}
	for _, tc := range []struct {
		name    string
		running int
		cpu     float64
		want    int
		why     string
	}{
		{"at target", 3, 50, 3, "nothing to do"},
		{"within tolerance", 3, 54, 3,
			"a ratio a few percent off target has to move nothing, or every pass asks for a different number for ever"},
		{"twice the target", 3, 100, 6, "three copies at 100 need six to sit at 50"},
		{"rounded up", 3, 60, 4,
			"3 * 1.2 is 3.6, and rounding down leaves every copy above target"},
		{"held at the ceiling", 6, 200, 8, "the ceiling is the point of having one"},
		{"held at the floor", 3, 5, 2, "and the floor is the point of that"},
		{"quiet", 4, 10, 2, "four copies at 10% do not need four"},
	} {
		if got := rule.Want(tc.running, tc.cpu); got != tc.want {
			t.Errorf("%s: %d copies at %.0f%% asked for %d, want %d — %s",
				tc.name, tc.running, tc.cpu, got, tc.want, tc.why)
		}
	}
}

// A rule that is off asks for nothing, whatever the numbers beside it
// say. Turning it off leaves a min and a cpu behind, and reading those
// would be acting on an answer somebody withdrew.
func TestARuleThatIsOffMovesNothing(t *testing.T) {
	off := Autoscale{Min: 2, Max: 0, CPU: 50}
	if got := off.Want(1, 900); got != 1 {
		t.Errorf("an app with autoscaling off was moved to %d copies", got)
	}
}

// What the daemon will not run. A rule with no ceiling is the one that
// matters: it turns a loop of requests into a loop of replicas.
func TestARuleThatCannotSettleIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		rule Autoscale
		ok   bool
	}{
		{"off", Autoscale{}, true},
		{"ordinary", Autoscale{Min: 1, Max: 4, CPU: 70}, true},
		{"no floor", Autoscale{Min: 0, Max: 4, CPU: 70}, false},
		{"floor above ceiling", Autoscale{Min: 5, Max: 4, CPU: 70}, false},
		{"no target", Autoscale{Min: 1, Max: 4}, false},
		{"a ceiling somebody typed wrong", Autoscale{Min: 1, Max: 5000, CPU: 70}, false},
		{"negative target", Autoscale{Min: 1, Max: 4, CPU: -1}, false},
	} {
		if got := tc.rule.Valid(); got != tc.ok {
			t.Errorf("%s: Valid() = %v, want %v", tc.name, got, tc.ok)
		}
	}
}

// **It goes up quickly and comes down slowly.** Being wrong in the two
// directions costs different things: an extra copy costs some memory,
// and one copy too few costs the app its latency at exactly the moment
// load is coming back.
func TestItComesDownMoreSlowlyThanItGoesUp(t *testing.T) {
	justNow := time.Now().Add(-AutoscaleCooldown - time.Second)
	if !ready(&justNow, false) {
		t.Error("it would not grow after its own cooldown")
	}
	if ready(&justNow, true) {
		t.Error("it shrank on the growing cooldown, which is how a rule chases a spike back down")
	}
	older := time.Now().Add(-AutoscaleDownAfter - time.Second)
	if !ready(&older, true) {
		t.Error("it would never shrink")
	}
	if !ready(nil, true) {
		t.Error("a rule that has never acted was made to wait for a change it never made")
	}
}
