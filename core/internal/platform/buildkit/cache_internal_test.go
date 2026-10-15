package buildkit

import (
	"context"
	"testing"

	"github.com/moby/buildkit/client"
)

// What a clean will free is the cache no build holds. A record a running
// build is using is neither shown as removable nor removed.
func TestOnlyRecordsNoBuildHoldsAreReclaimable(t *testing.T) {
	records := []*client.UsageInfo{
		{Size: 100},
		{Size: 50, InUse: true},
		{Size: 7},
	}
	if got := reclaimable(records); got != 107 {
		t.Errorf("reclaimable = %d, want 107", got)
	}
}

// A builder that was never started has no cache: the storage page reads
// zero and a clean does nothing, and neither starts a container.
func TestACacheWithNoBuilderIsEmpty(t *testing.T) {
	b := New("tcp://127.0.0.1:1", nil)
	b.EnsureRunning = func(context.Context) error {
		t.Error("reading or cleaning the cache started the builder")
		return nil
	}

	if got, err := b.CacheUsage(context.Background()); err != nil || got != 0 {
		t.Errorf("CacheUsage = %d, %v; want 0, nil", got, err)
	}
	if got, err := b.PruneCache(context.Background()); err != nil || got != 0 {
		t.Errorf("PruneCache = %d, %v; want 0, nil", got, err)
	}
}
