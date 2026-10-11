package buildkit

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/moby/buildkit/client"
)

// The layer cache lives in buildkitd's own store, not in the Engine, so
// `docker system df` reports it as zero and `docker builder prune` cannot
// reach it. These two methods are how the storage screen sees and empties
// it.
//
// Neither starts the builder: an instance that never built anything has
// no cache, and a privileged container started to say so would be the
// cost the lazy start exists to avoid.

// cacheTimeout bounds a size reading. Emptying has no bound but the
// caller's: removing tens of gigabytes of layers takes as long as it takes.
const cacheTimeout = 30 * time.Second

// CacheUsage is how many bytes of the builder's cache can be removed.
//
// A builder that is not running, or that has never run, has none: that is
// zero, not an error.
func (b *Builder) CacheUsage(ctx context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, cacheTimeout)
	defer cancel()

	c, err := client.New(ctx, b.addr)
	if err != nil {
		return 0, nil
	}
	defer c.Close()

	records, err := c.DiskUsage(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		log.Printf("buildkit: cannot read the cache size, taking it as empty: %v", err)
		return 0, nil
	}
	return reclaimable(records), nil
}

// PruneCache removes the cache nothing is using and returns the bytes it
// freed.
//
// buildkitd keeps a record a running build holds, so a build in flight
// loses nothing; the next one pays for the layers again. Only the
// builder's own store is touched: the images the Engine runs, volumes
// and backups are not in it.
func (b *Builder) PruneCache(ctx context.Context) (int64, error) {
	c, err := client.New(ctx, b.addr)
	if err != nil {
		return 0, nil
	}
	defer c.Close()

	// Asked first, so a builder that is not there is told apart from a
	// prune that failed.
	if _, err := c.DiskUsage(ctx); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		log.Printf("buildkit: not cleaning the cache, the builder does not answer: %v", err)
		return 0, nil
	}

	ch := make(chan client.UsageInfo)
	var removed []*client.UsageInfo
	done := make(chan struct{})
	go func() {
		defer close(done)
		for r := range ch {
			r := r
			removed = append(removed, &r)
		}
	}()
	// All, because the largest records are the ones BuildKit marks
	// internal (sources and base layers); without it the call frees a
	// fraction of what is held.
	err = c.Prune(ctx, ch, client.PruneAll)
	close(ch)
	<-done
	if err != nil {
		return sum(removed), fmt.Errorf("clean the builder cache: %w", err)
	}
	return sum(removed), nil
}

// reclaimable adds up the records no build is holding.
func reclaimable(records []*client.UsageInfo) int64 {
	var total int64
	for _, r := range records {
		if !r.InUse {
			total += r.Size
		}
	}
	return total
}

func sum(records []*client.UsageInfo) int64 {
	var total int64
	for _, r := range records {
		total += r.Size
	}
	return total
}
