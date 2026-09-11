package backup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cubeship/internal/platform/database"
)

type Repository struct{ q database.Queryer }

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// columns is the list every scan below reads in order. Change one,
// change both — and mind the queries that spell it out for a join,
// which is where this has gone wrong elsewhere.
const columns = `id, COALESCE(datastore_id, 0), datastore_name, engine, version,
	COALESCE(object_store_id, 0), bucket, object_key, size_bytes,
	status, error, scheduled, started_at, finished_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Backup, error) {
	var b Backup
	var finished sql.NullTime
	if err := row.Scan(&b.ID, &b.DatastoreID, &b.DatastoreName, &b.Engine, &b.Version,
		&b.StoreID, &b.Bucket, &b.Key, &b.Size,
		&b.Status, &b.Error, &b.Scheduled, &b.StartedAt, &finished); err != nil {
		return nil, err
	}
	if finished.Valid {
		b.FinishedAt = &finished.Time
	}
	return &b, nil
}

// Start writes the row a dump will report into, before it begins.
func (r *Repository) Start(ctx context.Context, b *Backup) (*Backup, error) {
	var datastoreID, storeID any
	if b.DatastoreID != 0 {
		datastoreID = b.DatastoreID
	}
	if b.StoreID != 0 {
		storeID = b.StoreID
	}
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO backups
		   (datastore_id, datastore_name, engine, version,
		    object_store_id, bucket, object_key, status, scheduled)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'running',$8)
		 RETURNING `+columns,
		datastoreID, b.DatastoreName, b.Engine, b.Version,
		storeID, b.Bucket, b.Key, b.Scheduled)
	created, err := scan(row)
	if err != nil {
		return nil, fmt.Errorf("start backup: %w", err)
	}
	return created, nil
}

// Finish records how it went. A failure keeps the row: a backup that
// did not happen is the thing somebody most needs to find out about,
// and a row that disappeared on failure is a schedule that looks like
// it is working.
func (r *Repository) Finish(ctx context.Context, id int64, size int64, failure string) error {
	status := StatusDone
	if failure != "" {
		status = StatusFailed
	}
	_, err := r.q.ExecContext(ctx,
		`UPDATE backups SET status = $1, error = $2, size_bytes = $3, finished_at = now()
		 WHERE id = $4`, status, failure, size, id)
	if err != nil {
		return fmt.Errorf("finish backup: %w", err)
	}
	return nil
}

func (r *Repository) ByID(ctx context.Context, id int64) (*Backup, error) {
	b, err := scan(r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM backups WHERE id = $1`, id))
	if err != nil {
		return nil, fmt.Errorf("get backup: %w", err)
	}
	return b, nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.q.ExecContext(ctx, `DELETE FROM backups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return database.ErrNotFound
	}
	return nil
}

// ForDatastore lists one database's backups, newest first.
func (r *Repository) ForDatastore(ctx context.Context, datastoreID int64) ([]*Backup, error) {
	return r.list(ctx, `SELECT `+columns+` FROM backups
		WHERE datastore_id = $1 ORDER BY started_at DESC`, datastoreID)
}

// List is every backup on the instance, newest first — including the
// ones whose database is gone, which are the only place those still
// exist.
func (r *Repository) List(ctx context.Context) ([]*Backup, error) {
	return r.list(ctx, `SELECT `+columns+` FROM backups ORDER BY started_at DESC`)
}

// Expired is what retention removes: everything past the newest `keep`
// that actually succeeded.
//
// **Counted among the successful ones only.** A week of failures would
// otherwise push the last good dump out of the window, which is the one
// moment retention must not be the thing that loses it. A failed row is
// kept regardless — it is the evidence that a schedule is not working.
func (r *Repository) Expired(ctx context.Context, datastoreID int64, keep int) ([]*Backup, error) {
	if keep <= 0 {
		return nil, nil
	}
	return r.list(ctx, `SELECT `+columns+` FROM backups
		WHERE datastore_id = $1 AND status = $2
		ORDER BY started_at DESC OFFSET $3`, datastoreID, StatusDone, keep)
}

func (r *Repository) list(ctx context.Context, query string, args ...any) ([]*Backup, error) {
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	defer rows.Close()

	out := []*Backup{}
	for rows.Next() {
		b, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list backups: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// --- schedules ---

const scheduleColumns = `datastore_id, at, timezone, keep,
	COALESCE(object_store_id, 0), bucket, last_run_at`

func scanSchedule(row scanner) (*Schedule, error) {
	var s Schedule
	var last sql.NullTime
	if err := row.Scan(&s.DatastoreID, &s.At, &s.Timezone, &s.Keep,
		&s.StoreID, &s.Bucket, &last); err != nil {
		return nil, err
	}
	if last.Valid {
		s.LastRunAt = &last.Time
	}
	return &s, nil
}

// SetSchedule writes one, or replaces the one that is there. One row
// per database, so this is an upsert rather than a create and an update
// that could disagree about which exists.
func (r *Repository) SetSchedule(ctx context.Context, s *Schedule) (*Schedule, error) {
	var storeID any
	if s.StoreID != 0 {
		storeID = s.StoreID
	}
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO backup_schedules (datastore_id, at, timezone, keep, object_store_id, bucket)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (datastore_id) DO UPDATE
		   SET at = $2, timezone = $3, keep = $4,
		       object_store_id = $5, bucket = $6, updated_at = now()
		 RETURNING `+scheduleColumns,
		s.DatastoreID, s.At, s.Timezone, s.Keep, storeID, s.Bucket)
	created, err := scanSchedule(row)
	if err != nil {
		return nil, fmt.Errorf("set backup schedule: %w", err)
	}
	return created, nil
}

// ScheduleFor returns one, or nothing at all — which is what off is.
func (r *Repository) ScheduleFor(ctx context.Context, datastoreID int64) (*Schedule, error) {
	s, err := scanSchedule(r.q.QueryRowContext(ctx,
		`SELECT `+scheduleColumns+` FROM backup_schedules WHERE datastore_id = $1`, datastoreID))
	if err != nil {
		return nil, fmt.Errorf("get backup schedule: %w", err)
	}
	return s, nil
}

func (r *Repository) DeleteSchedule(ctx context.Context, datastoreID int64) error {
	_, err := r.q.ExecContext(ctx,
		`DELETE FROM backup_schedules WHERE datastore_id = $1`, datastoreID)
	if err != nil {
		return fmt.Errorf("delete backup schedule: %w", err)
	}
	return nil
}

// Schedules is every one there is, for the loop that decides which are
// due.
func (r *Repository) Schedules(ctx context.Context) ([]*Schedule, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM backup_schedules`)
	if err != nil {
		return nil, fmt.Errorf("list backup schedules: %w", err)
	}
	defer rows.Close()

	out := []*Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("list backup schedules: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkRun records that the timer fired, whatever the dump then did.
//
// Written before the work rather than after it, so a dump that takes an
// hour — or a daemon that dies during one — cannot make the schedule
// fire again the moment it comes back.
func (r *Repository) MarkRun(ctx context.Context, datastoreID int64, at time.Time) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE backup_schedules SET last_run_at = $1 WHERE datastore_id = $2`, at, datastoreID)
	if err != nil {
		return fmt.Errorf("mark backup schedule: %w", err)
	}
	return nil
}
