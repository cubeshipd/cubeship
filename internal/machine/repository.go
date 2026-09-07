package machine

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// Insert records one pass.
//
// The rates are written as NULL when there is none — the first pass
// after a restart, or a daemon that cannot see the machine's
// interfaces. Zero would be a reading, and this is the absence of one.
func (r *Repository) Insert(ctx context.Context, s Sample) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO host_samples
		     (at, cpu_percent, memory_bytes, memory_total_bytes, disk_bytes, disk_total_bytes,
		      rx_bytes_per_sec, tx_bytes_per_sec)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.At, s.CPUPercent, s.MemoryBytes, s.MemoryTotalBytes, s.DiskBytes, s.DiskTotalBytes,
		nullable(s.RxBytesPerSec), nullable(s.TxBytesPerSec))
	if err != nil {
		return fmt.Errorf("record host sample: %w", err)
	}
	return nil
}

func nullable(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// Series is the machine's readings over window, averaged into buckets.
//
// Bucketed in SQL against a fixed origin, for the same two reasons the
// container series is: a day of raw rows is most of a megabyte to draw
// a line that cannot show the difference, and two charts loaded seconds
// apart must land on the same grid rather than each having its own.
//
// avg() skips nulls, so a bucket is only without a rate when every
// sample in it was without one.
func (r *Repository) Series(ctx context.Context, w metrics.Window, now time.Time) ([]Sample, error) {
	// The bucket is interpolated rather than parameterised, and it is
	// safe to be: it comes from metrics.Windows, a fixed list, never
	// from a request. ParseWindow is what turns a caller's string into
	// one of them.
	bucket := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	rows, err := r.q.QueryContext(ctx,
		`SELECT date_bin(interval '`+bucket+`', at, timestamptz '2000-01-01') AS bucket,
		        avg(cpu_percent), avg(memory_bytes)::bigint, max(memory_total_bytes),
		        avg(disk_bytes)::bigint, max(disk_total_bytes),
		        avg(rx_bytes_per_sec), avg(tx_bytes_per_sec)
		 FROM host_samples
		 WHERE at >= $1
		 GROUP BY bucket
		 ORDER BY bucket`,
		now.Add(-w.Span))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var s Sample
		var rx, tx sql.NullFloat64
		if err := rows.Scan(&s.At, &s.CPUPercent, &s.MemoryBytes, &s.MemoryTotalBytes,
			&s.DiskBytes, &s.DiskTotalBytes, &rx, &tx); err != nil {
			return nil, err
		}
		if rx.Valid {
			s.RxBytesPerSec = &rx.Float64
		}
		if tx.Valid {
			s.TxBytesPerSec = &tx.Float64
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Prune drops everything older than the retention every series here
// keeps. It runs on every collection pass rather than on a timer of its
// own — the pass is already the thing that knows time has moved.
func (r *Repository) Prune(ctx context.Context, before time.Time) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM host_samples WHERE at < $1`, before)
	if err != nil {
		return fmt.Errorf("prune host samples: %w", err)
	}
	return nil
}
