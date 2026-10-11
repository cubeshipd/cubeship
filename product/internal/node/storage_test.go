package node

import (
	"context"
	"errors"
	"testing"

	"cubeship/internal/platform/dockerx"
)

type fakeEngine struct {
	storage dockerx.Storage
	freed   int64
	err     error
	pruned  bool
}

func (f *fakeEngine) Storage(context.Context) (dockerx.Storage, error) { return f.storage, nil }
func (f *fakeEngine) Prune(context.Context) (int64, error) {
	f.pruned = true
	return f.freed, f.err
}

type fakeCache struct {
	usage  int64
	freed  int64
	err    error
	pruned bool
}

func (f *fakeCache) CacheUsage(context.Context) (int64, error) { return f.usage, f.err }
func (f *fakeCache) PruneCache(context.Context) (int64, error) {
	f.pruned = true
	return f.freed, f.err
}

// The builder's layers are what a build leaves on the disk, and the
// Engine reports none of them. The card has to show them, or the page
// says there is nothing to clean while the disk is full.
func TestTheBuilderCacheIsCountedAsBuildCache(t *testing.T) {
	engine := dockerx.Storage{BuildCacheBytes: 1 << 20, Images: 2}
	got := withBuilderCache(context.Background(), engine, &fakeCache{usage: 43 << 30})
	if want := int64(43<<30 + 1<<20); got.BuildCacheBytes != want {
		t.Errorf("BuildCacheBytes = %d, want %d", got.BuildCacheBytes, want)
	}
	if got.Images != 2 {
		t.Errorf("the Engine's own numbers changed: %+v", got)
	}
}

// A builder that does not answer is not a reason to hide the Engine's
// numbers: the page still loads.
func TestABuilderThatDoesNotAnswerLeavesTheEngineNumbers(t *testing.T) {
	engine := dockerx.Storage{BuildCacheBytes: 7}
	for name, cache := range map[string]BuilderCache{
		"none":   nil,
		"failed": &fakeCache{usage: 99, err: errors.New("down")},
	} {
		if got := withBuilderCache(context.Background(), engine, cache); got.BuildCacheBytes != 7 {
			t.Errorf("%s: BuildCacheBytes = %d, want 7", name, got.BuildCacheBytes)
		}
	}
}

// Clean frees the Engine's data and the builder's, and reports both.
func TestCleaningFreesTheBuilderCacheToo(t *testing.T) {
	engine := &fakeEngine{freed: 100}
	cache := &fakeCache{freed: 900}
	got, err := pruneStorage(context.Background(), engine, cache)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1000 || !engine.pruned || !cache.pruned {
		t.Errorf("reclaimed %d (engine pruned %v, builder pruned %v), want 1000 from both", got, engine.pruned, cache.pruned)
	}
}

// An Engine that failed to clean stops there: the builder is not touched
// after an error the person has not seen.
func TestAnEngineFailureStopsTheClean(t *testing.T) {
	cache := &fakeCache{freed: 900}
	if _, err := pruneStorage(context.Background(), &fakeEngine{err: errors.New("boom")}, cache); err == nil {
		t.Fatal("the failure was swallowed")
	}
	if cache.pruned {
		t.Error("the builder was cleaned after the Engine failed")
	}
}

// A builder that fails after the Engine was cleaned still says what was
// freed, so the number is not lost with the error.
func TestABuilderFailureKeepsWhatTheEngineFreed(t *testing.T) {
	got, err := pruneStorage(context.Background(), &fakeEngine{freed: 100}, &fakeCache{err: errors.New("down")})
	if err == nil {
		t.Fatal("the failure was swallowed")
	}
	if got != 100 {
		t.Errorf("reclaimed %d, want 100", got)
	}
}

func TestACleanWithoutABuilderFreesTheEngine(t *testing.T) {
	got, err := pruneStorage(context.Background(), &fakeEngine{freed: 100}, nil)
	if err != nil || got != 100 {
		t.Errorf("= %d, %v; want 100, nil", got, err)
	}
}
