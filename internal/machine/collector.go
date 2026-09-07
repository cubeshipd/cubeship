package machine

import (
	"context"
	"log"
	"runtime/debug"
	"time"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
)

// Collector takes one reading of the machine on a timer and writes it.
//
// Its own ticker rather than a place in the container collector's pass:
// that one asks the Engine about every container it is given, and this
// one reads four files. Nothing about them is the same work, and a
// wedged Engine call must not be why the machine's own chart has a hole
// in it.
//
// The interval and the retention are metrics' own, so an instance has
// one cadence and one horizon rather than two that drift.
type Collector struct {
	db     *database.DB
	reader *Reader

	// previous is the last raw reading, because a CPU percentage and a
	// byte rate are both differences. Nothing is written until there is
	// one, which is why a chart starts one interval after the daemon
	// does — a rate before then would be invented, and an invented
	// point is one somebody reads as a fact.
	previous   *reading
	unreadable bool
}

// reading is the machine as the kernel last reported it: counters, not
// rates.
type reading struct {
	at     time.Time
	cpu    CPUTime
	net    Net
	hasNet bool
}

func NewCollector(db *database.DB, reader *Reader) *Collector {
	return &Collector{db: db, reader: reader}
}

// Run samples until ctx is done. It is the daemon's own goroutine, so
// it recovers: a panic in here would take down every app this process
// proxies, which is a great deal worse than a gap in a chart.
func (c *Collector) Run(ctx context.Context) {
	ticker := time.NewTicker(metrics.Interval)
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
			log.Printf("host metrics: collection panicked: %v\n%s", r, debug.Stack())
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, metrics.Interval)
	defer cancel()

	if err := c.Collect(ctx); err != nil {
		log.Printf("host metrics: %v", err)
	}
}

// Collect takes one reading and writes it. Exported so a test can drive
// a pass without waiting for a ticker.
func (c *Collector) Collect(ctx context.Context) error {
	now := now()

	cpu, cpuErr := c.reader.CPU()
	mem, memErr := c.reader.Memory()
	disk, diskErr := c.reader.Disk()
	if cpuErr != nil || memErr != nil || diskErr != nil {
		// A machine this daemon cannot read at all: a Mac running
		// `make dev`, where there is no procfs to read. Said once, not
		// every thirty seconds for as long as the process lives — the
		// screen says the same thing where somebody is looking.
		if !c.unreadable {
			c.unreadable = true
			log.Printf("host metrics: not recording — this machine's numbers are not readable here (%v)",
				firstError(cpuErr, memErr, diskErr))
		}
		return nil
	}
	c.unreadable = false

	net, _, netErr := c.reader.Network()
	current := &reading{at: now, cpu: cpu, net: net, hasNet: netErr == nil}

	previous := c.previous
	c.previous = current
	if previous == nil {
		// The first pass of a daemon's life is a reading taken and not
		// written. Everything on this chart is a difference, and there
		// is nothing yet to take one against.
		return nil
	}

	sample := Sample{
		At:               now,
		CPUPercent:       cpuPercent(previous.cpu, cpu),
		MemoryBytes:      mem.Used,
		MemoryTotalBytes: mem.Total,
		DiskBytes:        disk.Used,
		DiskTotalBytes:   disk.Total,
	}
	if previous.hasNet && current.hasNet {
		elapsed := now.Sub(previous.at).Seconds()
		if rx, ok := Rate(previous.net.Rx, net.Rx, elapsed); ok {
			sample.RxBytesPerSec = &rx
		}
		if tx, ok := Rate(previous.net.Tx, net.Tx, elapsed); ok {
			sample.TxBytesPerSec = &tx
		}
	}

	repo := c.Repo()
	if err := repo.Insert(ctx, sample); err != nil {
		return err
	}
	return repo.Prune(ctx, now.Add(-metrics.Retention))
}

func (c *Collector) Repo() *Repository { return NewRepository(c.db) }

// cpuPercent is how much of the whole machine was busy between two
// readings — 100 is every core.
//
// Against the kernel's own total rather than against elapsed wall time
// and a core count: /proc/stat's fields already add up to the time
// every core spent somewhere, so the ratio needs neither of those and
// cannot disagree with them.
func cpuPercent(previous, current CPUTime) float64 {
	total := current.Total - previous.Total
	busy := current.Busy - previous.Busy
	if current.Total < previous.Total || total == 0 {
		// The counters went backwards, which is a reboot. No rate is
		// the honest answer, and zero is the one that draws.
		return 0
	}
	percent := float64(busy) / float64(total) * 100
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// now is time.Now, replaced in tests that need a series with a shape.
var now = time.Now
