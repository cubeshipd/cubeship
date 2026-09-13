package machine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database/dbtest"
)

// A pass, then another, against a machine whose counters moved between
// them.
//
// The SQL is the part only a real Postgres can check — date_bin, a
// GROUP BY on an output alias, casts around the averages, and avg()
// over a column that is sometimes null. None of it compiles wrong; it
// fails at run time on a box nobody is watching.
func TestAPassIsRecordedOnlyOnceThereIsSomethingToCompareAgainst(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()

	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), procStat)
	write(t, filepath.Join(proc, "meminfo"), procMeminfo)
	write(t, filepath.Join(proc, "net", "dev"), procNetDev)
	reader := newReaderAt(proc, t.TempDir(), t.TempDir(), false)
	c := NewCollector(db, reader)

	// The first pass of a daemon's life writes nothing. Every rate here
	// is a difference, and there is nothing yet to take one against —
	// a zero at the left edge of the chart after every restart is a
	// point somebody would read as a fact.
	if err := c.Collect(ctx); err != nil {
		t.Fatal(err)
	}
	series, err := NewRepository(db).Series(ctx, metrics.DefaultWindow, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Fatalf("the first pass wrote %d rows, want none", len(series))
	}

	// The machine moved: half of every tick busy, and 3000 more bytes
	// in on eth0.
	write(t, filepath.Join(proc, "stat"), `cpu  600 20 30 1300 40 0 10 5 60 3
cpu0 300 10 15 650 20 0 5 2 30 1
cpu1 300 10 15 650 20 0 5 3 30 2
`)
	write(t, filepath.Join(proc, "net", "dev"), `Inter-|
 face |bytes
    lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
  eth0: 8000 50 0 0 0 0 0 0 2600 20 0 0 0 0 0 0
   wg0: 300 3 0 0 0 0 0 0 100 1 0 0 0 0 0 0
`)
	if err := c.Collect(ctx); err != nil {
		t.Fatal(err)
	}

	series, err = NewRepository(db).Series(ctx, metrics.DefaultWindow, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("the second pass wrote %d rows, want one", len(series))
	}
	got := series[0]
	if got.CPUPercent <= 0 || got.CPUPercent > 100 {
		t.Errorf("cpu = %v, want a share of the machine", got.CPUPercent)
	}
	if got.MemoryTotalBytes != 2048000*1024 {
		t.Errorf("memory total = %d", got.MemoryTotalBytes)
	}
	if got.DiskTotalBytes <= 0 {
		t.Error("no disk was recorded for a directory that exists")
	}
	if got.RxBytesPerSec == nil || *got.RxBytesPerSec <= 0 {
		t.Errorf("rx = %v, want the 3000 bytes that arrived", got.RxBytesPerSec)
	}
	if got.TxBytesPerSec == nil || *got.TxBytesPerSec <= 0 {
		t.Errorf("tx = %v, want the 600 bytes that left", got.TxBytesPerSec)
	}
}

// A daemon that cannot see the machine's interfaces still records the
// three measurements it can take. The column is null rather than zero,
// because zero is a reading and this is the absence of one — and a
// chart drawn from zeros would say this instance moved no traffic all
// day.
func TestWithoutTheMachinesInterfacesTheRestIsStillRecorded(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()

	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), procStat)
	write(t, filepath.Join(proc, "meminfo"), procMeminfo)
	// In a container, with nothing mounted at the machine's procfs.
	reader := newReaderAt(proc, filepath.Join(t.TempDir(), "absent"), t.TempDir(), true)
	c := NewCollector(db, reader)

	for range 2 {
		if err := c.Collect(ctx); err != nil {
			t.Fatal(err)
		}
	}
	series, err := NewRepository(db).Series(ctx, metrics.DefaultWindow, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("wrote %d rows, want one", len(series))
	}
	if series[0].RxBytesPerSec != nil || series[0].TxBytesPerSec != nil {
		t.Errorf("a daemon that cannot see the interfaces reported %v/%v",
			series[0].RxBytesPerSec, series[0].TxBytesPerSec)
	}
	if series[0].MemoryTotalBytes == 0 {
		t.Error("the memory it could read was not recorded")
	}
}

// A machine whose numbers are not readable at all — a Mac running
// `make dev`, where there is no procfs. Nothing is written, because a
// row of zeros is four charts of lies.
func TestAMachineThatCannotBeReadRecordsNothing(t *testing.T) {
	dbtest.RequireDatabase(t)
	db := dbtest.New(t)
	ctx := context.Background()

	absent := filepath.Join(t.TempDir(), "absent")
	c := NewCollector(db, newReaderAt(absent, absent, absent, false))
	for range 2 {
		if err := c.Collect(ctx); err != nil {
			t.Fatal(err)
		}
	}
	series, err := NewRepository(db).Series(ctx, metrics.DefaultWindow, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 0 {
		t.Errorf("wrote %d rows about a machine it cannot read", len(series))
	}
}
