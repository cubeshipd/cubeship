package backup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Schedule reads one database's, or nothing at all — which is what off
// is, because the row existing is the whole of it.
func (s *Service) Schedule(ctx context.Context, caller *user.User, name string) (*Schedule, error) {
	d, err := s.resolve(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	schedule, err := s.Repo().ScheduleFor(ctx, d.ID)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return schedule, err
}

// SetSchedule turns one on, or moves it.
//
// Every refusal here happens where the person who typed it is still
// watching, rather than at three in the morning in a log nobody reads:
// a time that is not one, a zone this machine does not know, a count
// that is negative, and a store named with no bucket to put anything
// in.
func (s *Service) SetSchedule(ctx context.Context, caller *user.User, name, store string, in Schedule) (*Schedule, error) {
	d, err := s.resolve(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	if !d.Engine.CanBackUp() {
		return nil, fmt.Errorf("%w: %s", ErrNoDump, d.Engine)
	}
	if _, _, err := ParseTimeOfDay(in.At); err != nil {
		return nil, err
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	// Resolved here rather than trusted: the database is embedded in
	// the binary, so a name that fails is one nobody has — and finding
	// that out when the timer fires means a schedule that silently
	// never runs.
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTimezone, in.Timezone)
	}
	if in.Keep < 0 || in.Keep > MaxKeep {
		return nil, ErrBadKeep
	}
	if store != "" {
		if in.Bucket == "" {
			return nil, ErrNoBucket
		}
		in.StoreID, err = s.stores.IDForName(ctx, store)
		if err != nil {
			return nil, err
		}
		// Opened now, so a schedule cannot be written against a store
		// this instance cannot reach — which would be a row that fires
		// nightly and fails nightly, in a log nobody reads.
		if _, _, err := s.stores.ClientForID(ctx, in.StoreID); err != nil {
			return nil, err
		}
	} else {
		in.StoreID, in.Bucket = 0, ""
	}

	in.DatastoreID = d.ID
	return s.Repo().SetSchedule(ctx, &in)
}

// UnsetSchedule turns it off by removing the row, which is what off is.
func (s *Service) UnsetSchedule(ctx context.Context, caller *user.User, name string) error {
	d, err := s.resolve(ctx, caller, name)
	if err != nil {
		return err
	}
	return s.Repo().DeleteSchedule(ctx, d.ID)
}

// Due reports whether a schedule should fire, given when it last did.
//
// Kept apart from the loop so it can be read and tested against a clock
// somebody chooses. The rule is: the most recent occurrence of that
// time of day, in that zone, is in the past and has not been run.
//
// **A missed window runs late rather than being skipped.** A daemon
// that was down at 03:00 and came back at 04:00 takes the backup at
// 04:00, because a night with no backup is the thing this exists to
// prevent and being an hour off is not.
func Due(s *Schedule, now time.Time) bool {
	hour, minute, err := ParseTimeOfDay(s.At)
	if err != nil {
		return false
	}
	zone := s.Timezone
	if zone == "" {
		zone = "UTC"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return false
	}

	local := now.In(loc)
	occurrence := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if occurrence.After(local) {
		occurrence = occurrence.AddDate(0, 0, -1)
	}
	if s.LastRunAt == nil {
		return true
	}
	return s.LastRunAt.Before(occurrence)
}

// Scheduler takes the backups nobody asked for.
//
// It runs in cmd/cubeshipd rather than in server.New, for the reason
// every other loop here does: a server is a request handler, and a test
// that builds one must not thereby start dumping databases.
type Scheduler struct {
	Backups *Service
	// Interval is how often it asks. A minute by default, which is the
	// resolution a time of day needs and no finer — the alternative is
	// sleeping until the next occurrence, which a daemon restart, a
	// schedule being edited, and a clock changing all invalidate.
	Interval time.Duration
}

// CheckInterval is how often the loop looks.
const CheckInterval = time.Minute

func (s *Scheduler) Run(ctx context.Context) {
	every := s.Interval
	if every <= 0 {
		every = CheckInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.Once(ctx)
	}
}

// Once is one pass: take whatever is due, and prune what retention says
// is past.
func (s *Scheduler) Once(ctx context.Context) {
	schedules, err := s.Backups.Repo().Schedules(ctx)
	if err != nil {
		log.Printf("backup: reading the schedules: %v", err)
		return
	}
	now := time.Now()
	for _, schedule := range schedules {
		if !Due(schedule, now) {
			continue
		}
		s.Backups.RunScheduled(ctx, schedule, now)
	}
}

// RunScheduled takes one scheduled backup and prunes afterwards.
//
// **The schedule is marked before the dump starts**, not after. A dump
// that takes an hour — or a daemon that dies during one — would
// otherwise be due again the moment the loop came round, and an
// instance that had just restarted would take one backup per minute
// until it finished.
func (s *Service) RunScheduled(ctx context.Context, schedule *Schedule, at time.Time) {
	d, err := s.dbs.ByID(ctx, schedule.DatastoreID)
	if err != nil {
		log.Printf("backup: the database for a schedule is gone: %v", err)
		return
	}
	if err := s.Repo().MarkRun(ctx, schedule.DatastoreID, at); err != nil {
		log.Printf("backup: marking %s: %v", d.Slug, err)
		return
	}
	if _, err := s.start(ctx, d, schedule.StoreID, schedule.Bucket, true); err != nil {
		log.Printf("backup: starting one for %s: %v", d.Slug, err)
	}
}

// Prune removes what retention says is past, newest kept first.
//
// **Only successful backups are counted**, which is the decision worth
// keeping: a week of failures would otherwise push the last good dump
// out of the window, and that is exactly the moment retention must not
// be the thing that loses it. A failed row is never pruned either — it
// is the evidence that a schedule is not working, and an empty list of
// backups is a worse answer than a list of failures.
func (s *Service) Prune(ctx context.Context, schedule *Schedule) {
	if schedule.Keep <= 0 {
		return
	}
	expired, err := s.Repo().Expired(ctx, schedule.DatastoreID, schedule.Keep)
	if err != nil {
		log.Printf("backup: working out what to prune: %v", err)
		return
	}
	for _, row := range expired {
		if err := s.remove(ctx, row); err != nil {
			log.Printf("backup: removing %s: %v", row.Key, err)
			continue
		}
		if err := s.Repo().Delete(ctx, row.ID); err != nil {
			log.Printf("backup: clearing the row for %s: %v", row.Key, err)
		}
	}
}
