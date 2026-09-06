package metrics

import (
	"context"
	"testing"
	"time"

	"cubeship/internal/platform/database/dbtest"
)

// The series query is the one piece of this that only a real Postgres
// can check: date_bin, a GROUP BY on an output alias, and two casts
// around the averages. None of it compiles wrong — it fails at run
// time, on a machine nobody is watching.
//
// It is here because nothing else exercises it. The rest of this
// package is arithmetic with no database in it, which is exactly why
// the SQL needed its own test.
func TestASeriesIsBucketedAndAveraged(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()
	repo := NewRepository(db)

	// Four samples inside one 30-second bucket and one in the next, so
	// the average has something to average and the buckets have
	// something to separate.
	now := time.Now().Truncate(time.Minute)
	write := func(at time.Time, cpu float64, mem int64) {
		t.Helper()
		if err := repo.Insert(ctx, KindApp, 1, Sample{
			At: at, CPUPercent: cpu, MemoryBytes: mem, MemoryLimitBytes: 2048,
		}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	write(now.Add(-90*time.Second), 10, 100)
	write(now.Add(-85*time.Second), 30, 300)
	write(now.Add(-50*time.Second), 50, 500)

	// Another subject, and another kind with the same id, so the filter
	// has something to get wrong.
	if err := repo.Insert(ctx, KindApp, 2, Sample{At: now, CPUPercent: 999, MemoryBytes: 9}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.Insert(ctx, KindDatastore, 1, Sample{At: now, CPUPercent: 888, MemoryBytes: 8}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := repo.Series(ctx, KindApp, 1, DefaultWindow, now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d buckets, want 2: %+v", len(got), got)
	}
	if got[0].CPUPercent != 20 {
		t.Errorf("first bucket averaged to %v, want 20 — the mean of 10 and 30", got[0].CPUPercent)
	}
	if got[0].MemoryBytes != 200 {
		t.Errorf("first bucket memory is %d, want 200", got[0].MemoryBytes)
	}
	if got[1].CPUPercent != 50 {
		t.Errorf("second bucket is %v, want the lone 50", got[1].CPUPercent)
	}
	if got[0].MemoryLimitBytes != 2048 {
		t.Errorf("the ceiling came back as %d", got[0].MemoryLimitBytes)
	}
	// Ordered oldest first, because that is the direction a chart draws.
	if !got[0].At.Before(got[1].At) {
		t.Errorf("buckets came back newest first: %v then %v", got[0].At, got[1].At)
	}
}

// A window asks for a span, and anything older than it is not part of
// the answer.
func TestASeriesStopsAtTheWindow(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()
	repo := NewRepository(db)

	now := time.Now()
	inside := Sample{At: now.Add(-30 * time.Minute), CPUPercent: 1, MemoryBytes: 1}
	outside := Sample{At: now.Add(-3 * time.Hour), CPUPercent: 2, MemoryBytes: 2}
	for _, s := range []Sample{inside, outside} {
		if err := repo.Insert(ctx, KindApp, 1, s); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	hour, err := repo.Series(ctx, KindApp, 1, Windows[0], now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(hour) != 1 {
		t.Errorf("the 1h window returned %d buckets, want only the one inside it", len(hour))
	}

	// And the wider one reaches both.
	day, err := repo.Series(ctx, KindApp, 1, Windows[len(Windows)-1], now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(day) != 2 {
		t.Errorf("the 24h window returned %d buckets, want both", len(day))
	}
}

// The collection pass writes every subject at once, and prunes what has
// aged out. Both are statements no unit test above this reaches.
func TestAPassWritesTogetherAndPrunesWhatAgedOut(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()
	repo := NewRepository(db)

	now := time.Now()
	err := repo.InsertMany(ctx,
		[]string{KindApp, KindDatastore},
		[]int64{1, 1},
		[]Sample{
			{At: now, CPUPercent: 5, MemoryBytes: 50, MemoryLimitBytes: 500},
			{At: now, CPUPercent: 6, MemoryBytes: 60, MemoryLimitBytes: 600},
		})
	if err != nil {
		t.Fatalf("InsertMany: %v", err)
	}
	for _, kind := range []string{KindApp, KindDatastore} {
		got, err := repo.Series(ctx, kind, 1, DefaultWindow, now)
		if err != nil {
			t.Fatalf("Series(%s): %v", kind, err)
		}
		if len(got) != 1 {
			t.Errorf("%s got %d buckets, want 1 — the two kinds share an id and must not share rows", kind, len(got))
		}
	}
	// An empty pass is a no-op, not a malformed statement with no
	// values in it.
	if err := repo.InsertMany(ctx, nil, nil, nil); err != nil {
		t.Errorf("an empty pass errored: %v", err)
	}

	if err := repo.Insert(ctx, KindApp, 1, Sample{At: now.Add(-Retention - time.Hour)}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := repo.Prune(ctx, now.Add(-Retention)); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	got, err := repo.Series(ctx, KindApp, 1, Windows[len(Windows)-1], now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("after pruning there are %d buckets, want the one inside retention", len(got))
	}
}

// Ids come from sequences and are reused across tables, so a deleted
// app's history must go with it rather than become some later app's
// chart.
func TestForgettingASubjectLeavesTheOthers(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()
	repo := NewRepository(db)
	svc := NewService(db)

	now := time.Now()
	for _, kind := range []string{KindApp, KindDatastore} {
		if err := repo.Insert(ctx, kind, 7, Sample{At: now, CPUPercent: 1}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}
	if err := svc.Forget(ctx, KindApp, 7); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	gone, err := repo.Series(ctx, KindApp, 7, DefaultWindow, now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(gone) != 0 {
		t.Errorf("the app's history survived being forgotten: %+v", gone)
	}
	kept, err := repo.Series(ctx, KindDatastore, 7, DefaultWindow, now)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(kept) != 1 {
		t.Errorf("forgetting one kind took the other's rows with it")
	}
}

// The empty series is what a client draws a chart from, so it has to be
// an empty list rather than null — and it has to say which kind of
// empty it is.
func TestAnEmptySeriesSaysWhichKindOfEmptyItIs(t *testing.T) {
	dbtest.RequireDatabase(t)
	svc := NewService(dbtest.New(t))

	waiting, err := svc.Series(context.Background(), KindApp, 1, "", true)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if waiting.Samples == nil {
		t.Error("samples came back null, which is a different bug in every client")
	}
	if !waiting.Collecting {
		t.Error("a running container should report that it is being collected from")
	}
	if waiting.Window != DefaultWindow.Name {
		t.Errorf("an empty window resolved to %q", waiting.Window)
	}

	if _, err := svc.Series(context.Background(), KindApp, 1, "7d", true); err == nil {
		t.Error("an unknown window was accepted")
	}
}
