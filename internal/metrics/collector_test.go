package metrics

import (
	"context"
	"errors"
	"testing"

	"cubeship/internal/platform/dockerx"
)

type fakeStats struct {
	byContainer map[string]dockerx.Stats
	err         error
	asked       []string
}

func (f *fakeStats) ContainerStats(_ context.Context, id string) (dockerx.Stats, error) {
	f.asked = append(f.asked, id)
	if f.err != nil {
		return dockerx.Stats{}, f.err
	}
	s, ok := f.byContainer[id]
	if !ok {
		return dockerx.Stats{}, errors.New("no such container")
	}
	return s, nil
}

// A CPU percentage is a difference, and the Engine's one-shot has
// nothing to subtract from. The collector holds the previous reading,
// which means the first one has no answer — and the answer to that has
// to be zero rather than a guess, because an invented first point is a
// point somebody reads as a fact.
func TestTheFirstReadingHasNoCPUPercentage(t *testing.T) {
	c := NewCollector(nil, nil)

	first := c.cpuPercent("abc", dockerx.Stats{CPUTotal: 1000, CPUSystem: 10_000, OnlineCPUs: 2})
	if first != 0 {
		t.Errorf("first reading reported %v%%, with nothing to compare against", first)
	}

	// Half of one core over the interval, on a two-core host: the
	// container used 1000ns while the host used 4000ns across 2 cores.
	c.previous["abc"] = dockerx.Stats{CPUTotal: 1000, CPUSystem: 10_000, OnlineCPUs: 2}
	got := c.cpuPercent("abc", dockerx.Stats{CPUTotal: 2000, CPUSystem: 14_000, OnlineCPUs: 2})
	if want := 1000.0 / 4000.0 * 2 * 100; got != want {
		t.Errorf("cpuPercent = %v, want %v", got, want)
	}
}

// 100% means one core, not one machine. A container saturating four
// cores of an eight-core host reads as 400%, because rescaling it to
// "50% of the box" hides how much work it is doing behind how large the
// box is.
func TestOneHundredPercentIsOneCore(t *testing.T) {
	c := NewCollector(nil, nil)
	c.previous["abc"] = dockerx.Stats{CPUTotal: 0, CPUSystem: 0, OnlineCPUs: 8}

	// The container used every nanosecond the host did on 4 of 8 cores.
	got := c.cpuPercent("abc", dockerx.Stats{CPUTotal: 4000, CPUSystem: 8000, OnlineCPUs: 8})
	if got != 400 {
		t.Errorf("cpuPercent = %v, want 400 for four saturated cores", got)
	}
}

// A container that restarted under the same id, or an Engine that reset
// its counters, produces a negative difference. Reported as a spike it
// would be the largest point on every chart it ever appeared in.
func TestACounterGoingBackwardsIsNotASpike(t *testing.T) {
	c := NewCollector(nil, nil)
	c.previous["abc"] = dockerx.Stats{CPUTotal: 9000, CPUSystem: 90_000, OnlineCPUs: 2}

	if got := c.cpuPercent("abc", dockerx.Stats{CPUTotal: 10, CPUSystem: 100, OnlineCPUs: 2}); got != 0 {
		t.Errorf("cpuPercent = %v after the counters reset, want 0", got)
	}
}

// The window a caller asks for decides how far back and how coarse, and
// a name this release does not offer is refused rather than rounded: a
// chart labelled 6h showing one hour is worse than an error.
func TestEveryWindowIsBucketedToAboutTheSameNumberOfPoints(t *testing.T) {
	for _, w := range Windows {
		points := int(w.Span / w.Bucket)
		if points < TargetPoints/2 || points > TargetPoints*2 {
			t.Errorf("%s buckets to %d points, nowhere near %d", w.Name, points, TargetPoints)
		}
		got, err := ParseWindow(w.Name)
		if err != nil || got != w {
			t.Errorf("ParseWindow(%q) = %v, %v", w.Name, got, err)
		}
	}
	if got, err := ParseWindow(""); err != nil || got != DefaultWindow {
		t.Errorf("an empty window should be the default, got %v %v", got, err)
	}
	if _, err := ParseWindow("7d"); !errors.Is(err, ErrUnknownWindow) {
		t.Errorf("ParseWindow(7d) = %v, want ErrUnknownWindow", err)
	}
}

// Nothing sampled and nothing to sample are different sentences, and a
// chart that cannot tell them apart tells somebody to keep waiting for
// a container that is not running.
func TestRetentionOutlastsTheLongestWindow(t *testing.T) {
	longest := Windows[len(Windows)-1].Span
	if Retention <= longest {
		t.Errorf("retention is %v but the longest window asks for %v, so it would always be short", Retention, longest)
	}
}
