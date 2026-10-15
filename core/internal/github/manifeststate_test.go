package github

import (
	"testing"
	"time"
)

// The nonce is the whole of what separates a registration this instance
// started from a link somebody sent an admin, so each of the ways one
// can fail to be that is checked here.
func TestAManifestStateIsSingleUseAndBelongsToOneCaller(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var states manifestStates

	state, err := states.issue(7, false, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, ok := states.consume(state, 9, now); ok {
		t.Error("another account spent a state issued to somebody else")
	}
	if _, ok := states.consume("", 7, now); ok {
		t.Error("an empty state was accepted")
	}
	if _, ok := states.consume(state+"x", 7, now); ok {
		t.Error("a state nobody issued was accepted")
	}

	// Still good after those: a caller it does not belong to must not be
	// able to cancel somebody's registration by guessing at it.
	if _, ok := states.consume(state, 7, now); !ok {
		t.Fatal("the state its own caller issued was refused")
	}
	if _, ok := states.consume(state, 7, now); ok {
		t.Error("a state was spent twice; the exchange behind it is single-use")
	}

	expiring, err := states.issue(7, true, now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, ok := states.consume(expiring, 7, now.Add(ManifestStateTTL+time.Second)); ok {
		t.Error("an expired state was accepted")
	}
}

// Whether a registration replaces the App an instance already has is
// decided when the flow starts, not by the redirect that comes back.
func TestAManifestStateCarriesWhetherItMayReplace(t *testing.T) {
	now := time.Now()
	var states manifestStates

	plain, _ := states.issue(1, false, now)
	replacing, _ := states.issue(1, true, now)

	if got, _ := states.consume(plain, 1, now); got.replace {
		t.Error("a registration that did not ask to replace came back saying it may")
	}
	if got, _ := states.consume(replacing, 1, now); !got.replace {
		t.Error("a registration that asked to replace lost it")
	}
}
