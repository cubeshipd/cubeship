// Package metrics records what each container on this instance is
// using, and answers the time series a chart is drawn from.
//
// It is a module of its own because two others need exactly the same
// thing: an app and a datastore are both a container with a CPU and a
// resident set. Neither owns this, and neither should have to know how
// the other does it.
//
// It knows nothing about apps or datastores. A Subject is a kind, an
// id and a container — the modules that have those hand them over
// through Source, and mount the read endpoint at their own addresses,
// where they have already decided who is allowed to look.
package metrics

import (
	"errors"
	"time"
)

// Kinds of subject. They are the values in the kind column, so adding
// one is a decision about what a series belongs to.
const (
	KindApp       = "app"
	KindDatastore = "datastore"
)

// Subject is one thing worth sampling: what it is, which one, and the
// container currently serving it.
type Subject struct {
	Kind        string
	ID          int64
	ContainerID string
}

// Sample is one reading, already turned into the numbers a chart shows.
//
// A percentage rather than the counters it came from, because the
// counters only mean anything next to their predecessor — and after a
// daemon restart there is no predecessor. Doing the subtraction at
// collection time is what keeps a gap in the series from becoming a
// spike in it.
type Sample struct {
	At time.Time `json:"at"`
	// CPUPercent is percent of one core: 250 is two and a half cores.
	CPUPercent float64 `json:"cpu_percent"`
	// MemoryBytes is usage minus reclaimable page cache — what `docker
	// stats` shows.
	MemoryBytes int64 `json:"memory_bytes"`
	// MemoryLimitBytes is the cgroup's ceiling, or the host's memory
	// for a container with no limit of its own.
	MemoryLimitBytes int64 `json:"memory_limit_bytes"`
}

// Interval is how often every container is sampled.
//
// Thirty seconds is a compromise with one real constraint on each side:
// often enough that a spike lasting a minute is two points rather than
// one, and rare enough that a box with twenty containers is not doing
// an Engine round trip every second forever.
const Interval = 30 * time.Second

// Retention is how long a sample is kept.
//
// A day, and no more, because there is no downsampling behind it: these
// are raw rows in the instance's own database, and a week of them at
// this interval is twenty thousand rows per container that nothing ever
// looks at. What a day buys is the question anybody actually asks —
// "what happened overnight".
const Retention = 25 * time.Hour

// Window is how much of the past a series covers. Each has a bucket
// size chosen to land near TargetPoints, so a chart is the same density
// whichever is picked.
type Window struct {
	Name   string
	Span   time.Duration
	Bucket time.Duration
}

// TargetPoints is roughly how many points a series should carry: enough
// that a line has shape, few enough that the browser is not drawing a
// path with three thousand segments in it.
const TargetPoints = 120

// Windows are what a caller may ask for, shortest first. The first is
// the default.
var Windows = []Window{
	{Name: "1h", Span: time.Hour, Bucket: 30 * time.Second},
	{Name: "6h", Span: 6 * time.Hour, Bucket: 3 * time.Minute},
	{Name: "24h", Span: 24 * time.Hour, Bucket: 12 * time.Minute},
}

// DefaultWindow is what a request with no window asks for.
var DefaultWindow = Windows[0]

// ErrUnknownWindow is a window this version does not offer. Refused
// rather than rounded to the nearest, because a chart labelled "6h"
// showing one hour is worse than an error.
var ErrUnknownWindow = errors.New("unknown window")

// ParseWindow resolves a window by name. An empty name is the default.
func ParseWindow(name string) (Window, error) {
	if name == "" {
		return DefaultWindow, nil
	}
	for _, w := range Windows {
		if w.Name == name {
			return w, nil
		}
	}
	return Window{}, ErrUnknownWindow
}

// Series is one subject's readings over one window, with what the
// client needs to draw axes without deriving it.
type Series struct {
	Window  string   `json:"window"`
	Samples []Sample `json:"samples"`
	// MemoryLimitBytes is the ceiling the last sample saw, which is
	// what a memory chart is drawn against. Zero when nothing has been
	// sampled yet.
	MemoryLimitBytes int64 `json:"memory_limit_bytes"`
	// Collecting reports whether this subject is being sampled at all.
	// A container that is not running has no samples and never will
	// have any until it starts — which is a different sentence from
	// "nothing has happened yet", and the only one worth showing.
	Collecting bool `json:"collecting"`
}
