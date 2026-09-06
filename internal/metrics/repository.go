package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository {
	return &Repository{q: q}
}

// Insert records one reading.
func (r *Repository) Insert(ctx context.Context, kind string, subjectID int64, s Sample) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO metric_samples (kind, subject_id, at, cpu_percent, memory_bytes, memory_limit_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		kind, subjectID, s.At, s.CPUPercent, s.MemoryBytes, s.MemoryLimitBytes)
	if err != nil {
		return fmt.Errorf("record sample: %w", err)
	}
	return nil
}

// InsertMany records a whole collection pass in one statement.
//
// One round trip rather than one per container: a pass over twenty
// containers is twenty inserts of six small numbers, and the round
// trips cost more than the rows.
//
// A multi-row VALUES built here rather than array parameters, because
// the placeholders are the same ones every other statement in this
// codebase uses and there is nothing to get wrong about how a driver
// encodes a slice.
func (r *Repository) InsertMany(ctx context.Context, kinds []string, ids []int64, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	var values strings.Builder
	args := make([]any, 0, len(samples)*6)
	for i, s := range samples {
		if i > 0 {
			values.WriteString(", ")
		}
		n := i * 6
		fmt.Fprintf(&values, "($%d, $%d, $%d, $%d, $%d, $%d)",
			n+1, n+2, n+3, n+4, n+5, n+6)
		args = append(args, kinds[i], ids[i], s.At, s.CPUPercent, s.MemoryBytes, s.MemoryLimitBytes)
	}
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO metric_samples (kind, subject_id, at, cpu_percent, memory_bytes, memory_limit_bytes)
		 VALUES `+values.String(), args...)
	if err != nil {
		return fmt.Errorf("record samples: %w", err)
	}
	return nil
}

// Series returns one subject's readings over window, averaged into
// buckets.
//
// Bucketed in SQL rather than in the browser: a day at the sampling
// interval is nearly three thousand rows, and sending all of them to
// draw a hundred-and-twenty-point line is most of a megabyte for a
// picture that cannot show the difference.
//
// date_bin's origin is fixed rather than "now", so two charts loaded a
// few seconds apart line up instead of each having its own grid.
func (r *Repository) Series(ctx context.Context, kind string, subjectID int64, w Window, now time.Time) ([]Sample, error) {
	// The bucket goes into the statement rather than a parameter, and
	// it is safe to: it comes from Windows, a fixed list in this
	// package, never from a request. ParseWindow is what turns a
	// caller's string into one of them.
	bucket := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	rows, err := r.q.QueryContext(ctx,
		`SELECT date_bin(interval '`+bucket+`', at, timestamptz '2000-01-01') AS bucket,
		        avg(cpu_percent), avg(memory_bytes)::bigint, max(memory_limit_bytes)
		 FROM metric_samples
		 WHERE kind = $1 AND subject_id = $2 AND at >= $3
		 GROUP BY bucket
		 ORDER BY bucket`,
		kind, subjectID, now.Add(-w.Span))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var s Sample
		if err := rows.Scan(&s.At, &s.CPUPercent, &s.MemoryBytes, &s.MemoryLimitBytes); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteForSubject removes a subject's history. Called when the thing
// it measured is deleted, so an id reused later does not inherit
// somebody else's chart.
func (r *Repository) DeleteForSubject(ctx context.Context, kind string, subjectID int64) error {
	_, err := r.q.ExecContext(ctx,
		`DELETE FROM metric_samples WHERE kind = $1 AND subject_id = $2`, kind, subjectID)
	if err != nil {
		return fmt.Errorf("delete samples: %w", err)
	}
	return nil
}

// Prune drops everything older than Retention. It runs on every
// collection pass rather than on a timer of its own — the pass is
// already the thing that knows time has moved.
func (r *Repository) Prune(ctx context.Context, before time.Time) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM metric_samples WHERE at < $1`, before)
	if err != nil {
		return fmt.Errorf("prune samples: %w", err)
	}
	return nil
}
