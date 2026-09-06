package metrics

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
)

// StatsAPI is the one thing the collector needs from Docker.
// *dockerx.Client satisfies it; a test supplies a fake.
type StatsAPI interface {
	ContainerStats(ctx context.Context, containerID string) (dockerx.Stats, error)
}

// Source is a module that has containers worth sampling.
//
// The direction is what keeps this package from knowing what an app or
// a datastore is: they hand over a kind, an id and a container, and
// nothing else about themselves travels. The daemon wires them in.
type Source interface {
	MetricSubjects(ctx context.Context) ([]Subject, error)
}

// Collector samples every subject on a timer and writes the readings.
type Collector struct {
	db      *database.DB
	docker  StatsAPI
	sources []Source

	// previous holds the last raw counters per container, because a CPU
	// percentage is a difference and the Engine's one-shot has nothing
	// to subtract from. Keyed by container id rather than by subject:
	// a redeployed app is a new container, and comparing across the
	// swap would produce one impossible reading.
	// The elapsed time is not held beside it and does not need to be:
	// the percentage is a ratio of two deltas, and the host's own CPU
	// counter is what carries how much time went by. A pass that ran
	// late produces a correct average over the longer gap rather than a
	// spike.
	previous map[string]dockerx.Stats
}

func NewCollector(db *database.DB, docker StatsAPI, sources ...Source) *Collector {
	return &Collector{
		db: db, docker: docker, sources: sources,
		previous: map[string]dockerx.Stats{},
	}
}

// Run samples until ctx is done. It is the daemon's own goroutine, so
// it recovers: a panic in here would take down every app this process
// proxies, which is a great deal worse than a gap in a chart.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

func (c *Collector) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("metrics: collection panicked: %v\n%s", r, debug.Stack())
		}
	}()
	// Bounded well inside the interval, so a wedged Engine call delays
	// one pass instead of stopping every pass after it.
	ctx, cancel := context.WithTimeout(ctx, Interval)
	defer cancel()

	if err := c.Collect(ctx); err != nil {
		log.Printf("metrics: %v", err)
	}
}

// Collect takes one reading of every subject and writes them together.
// Exported so a test can drive a pass without waiting for a ticker.
func (c *Collector) Collect(ctx context.Context) error {
	now := time.Now()

	var subjects []Subject
	for _, source := range c.sources {
		found, err := source.MetricSubjects(ctx)
		if err != nil {
			// One module failing is not a reason to lose the others'
			// readings for this pass.
			log.Printf("metrics: listing subjects: %v", err)
			continue
		}
		subjects = append(subjects, found...)
	}

	var kinds []string
	var ids []int64
	var samples []Sample
	live := map[string]bool{}

	for _, s := range subjects {
		if s.ContainerID == "" {
			continue
		}
		live[s.ContainerID] = true

		stats, err := c.docker.ContainerStats(ctx, s.ContainerID)
		if err != nil {
			// A container that has just been removed is the common
			// case here, and it is not worth a line in the log every
			// thirty seconds.
			delete(c.previous, s.ContainerID)
			continue
		}

		sample := Sample{
			At:               now,
			MemoryBytes:      int64(stats.MemoryBytes),
			MemoryLimitBytes: int64(stats.MemoryLimit),
			CPUPercent:       c.cpuPercent(s.ContainerID, stats),
		}
		c.previous[s.ContainerID] = stats

		kinds = append(kinds, s.Kind)
		ids = append(ids, s.ID)
		samples = append(samples, sample)
	}

	// Containers that have gone stop being compared against. Left in,
	// the map would grow with every deploy for as long as the daemon
	// runs.
	for id := range c.previous {
		if !live[id] {
			delete(c.previous, id)
		}
	}

	repo := NewRepository(c.db)
	if err := repo.InsertMany(ctx, kinds, ids, samples); err != nil {
		return err
	}
	return repo.Prune(ctx, now.Add(-Retention))
}

// cpuPercent is the share of one core used since the previous reading,
// so 250 means two and a half cores.
//
// Zero when there is nothing to compare against — the first pass after
// a daemon start, or after a redeploy. Zero rather than a guess: an
// invented first point is a point somebody would read as a fact.
func (c *Collector) cpuPercent(containerID string, now dockerx.Stats) float64 {
	prev, ok := c.previous[containerID]
	if !ok {
		return 0
	}
	// A counter that went backwards is a container that restarted under
	// the same id, or an Engine that reset them. Either way the
	// difference is meaningless.
	if now.CPUTotal < prev.CPUTotal || now.CPUSystem < prev.CPUSystem {
		return 0
	}
	cpuDelta := float64(now.CPUTotal - prev.CPUTotal)
	systemDelta := float64(now.CPUSystem - prev.CPUSystem)
	if systemDelta <= 0 || cpuDelta <= 0 {
		return 0
	}
	return cpuDelta / systemDelta * float64(now.OnlineCPUs) * 100
}
