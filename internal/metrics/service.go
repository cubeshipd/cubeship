package metrics

import (
	"context"
	"sort"
	"time"

	"cubeship/internal/platform/database"
)

// Service answers the series a chart is drawn from.
//
// It takes no caller and checks no role, deliberately. A series belongs
// to an app or a datastore, and the module that owns one has already
// decided who may look at it — asking again here would be a second
// answer to a question with one right answer.
type Service struct {
	db *database.DB
	// sources are the modules that have containers, for the one
	// question that is about all of them at once: what is using this
	// machine. They are handed over at wiring time, the same seam
	// project.AppTeardown and credential.Dependant use and for the same
	// reason — the modules that own the rows sit above this one.
	sources []Source
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

// SetSources wires in the modules that have containers. Called once, by
// server.New.
func (s *Service) SetSources(sources ...Source) { s.sources = sources }

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Series reads one subject's readings over the named window.
//
// collecting is what the caller knows and this does not: whether there
// is a container behind this subject right now. An empty series means
// two different things — nothing has been sampled yet, or there is
// nothing to sample — and only one of them is worth waiting for.
func (s *Service) Series(ctx context.Context, kind string, subjectID int64, window string, collecting bool) (Series, error) {
	w, err := ParseWindow(window)
	if err != nil {
		return Series{}, err
	}
	samples, err := s.Repo().Series(ctx, kind, subjectID, w, time.Now())
	if err != nil {
		return Series{}, err
	}
	out := Series{Window: w.Name, Samples: samples, Collecting: collecting}
	if out.Samples == nil {
		// An empty list rather than null: every client of this draws a
		// chart from it, and `null.length` is a different bug in each
		// of them.
		out.Samples = []Sample{}
	}
	if n := len(samples); n > 0 {
		out.MemoryLimitBytes = samples[n-1].MemoryLimitBytes
	}
	return out, nil
}

// UsageWindow is how far back a reading may be and still count as "now".
//
// Two intervals: one pass may be missed — an Engine call that took too
// long, a daemon that was restarting — without a container dropping off
// the list and reappearing, and nothing older than that is what
// anything is using at this moment.
const UsageWindow = 2 * Interval

// Usage is what every container on this instance is using right now,
// heaviest CPU first.
//
// The names come from the modules rather than from a join: this package
// has no table to join against — an app, a database and a store are
// three of them — and the modules already hand over a name with the id
// they hand over. A reading whose subject is not in that list is
// dropped, which is exactly a container that has since gone.
func (s *Service) Usage(ctx context.Context) ([]Usage, error) {
	readings, err := s.Repo().Latest(ctx, time.Now().Add(-UsageWindow))
	if err != nil {
		return nil, err
	}

	names := map[string]string{}
	for _, source := range s.sources {
		subjects, err := source.MetricSubjects(ctx)
		if err != nil {
			// One module failing is not a reason to answer nothing
			// about the others.
			continue
		}
		for _, subject := range subjects {
			names[subjectKey(subject.Kind, subject.ID)] = subject.Name
		}
	}

	out := make([]Usage, 0, len(readings))
	for _, u := range readings {
		name, live := names[u.Name]
		if !live || name == "" {
			continue
		}
		u.Name = name
		out = append(out, u)
	}
	// Heaviest first, and memory breaks the tie: a machine's whole
	// container list at rest is a column of zeroes, and ordering that
	// by name would bury the one holding two gigabytes.
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPUPercent != out[j].CPUPercent {
			return out[i].CPUPercent > out[j].CPUPercent
		}
		return out[i].MemoryBytes > out[j].MemoryBytes
	})
	return out, nil
}

// Forget drops a subject's history, for when the thing it measured is
// deleted. Ids are sequences and do get reused across tables; a chart
// inheriting a stranger's history would be worse than an empty one.
func (s *Service) Forget(ctx context.Context, kind string, subjectID int64) error {
	return s.Repo().DeleteForSubject(ctx, kind, subjectID)
}
