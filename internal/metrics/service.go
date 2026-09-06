package metrics

import (
	"context"
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
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

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

// Forget drops a subject's history, for when the thing it measured is
// deleted. Ids are sequences and do get reused across tables; a chart
// inheriting a stranger's history would be worse than an empty one.
func (s *Service) Forget(ctx context.Context, kind string, subjectID int64) error {
	return s.Repo().DeleteForSubject(ctx, kind, subjectID)
}
