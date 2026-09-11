package backup_test

import (
	"sort"
	"testing"
	"time"

	"cubeship/internal/backup"
)

// at is a moment, written the way a test reads.
func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ran(s string) *time.Time {
	t := at(s)
	return &t
}

// Due is the whole of the timer, and both ways of getting it wrong cost
// somebody something: too eager takes a backup every minute, and too
// shy means a night with none.
//
// No database and no Docker — this is arithmetic against a clock the
// test chooses, which is exactly why it is a function rather than a
// branch inside the loop.
func TestWhenASchedduleIsDue(t *testing.T) {
	cases := []struct {
		name     string
		schedule backup.Schedule
		now      time.Time
		want     bool
	}{
		{
			// Nothing has run, so whatever the hour, one is owed.
			name:     "never run",
			schedule: backup.Schedule{At: "03:00", Timezone: "UTC"},
			now:      at("2026-09-11T00:30:00Z"),
			want:     true,
		},
		{
			name:     "already run since this morning's occurrence",
			schedule: backup.Schedule{At: "03:00", Timezone: "UTC", LastRunAt: ran("2026-09-11T03:00:05Z")},
			now:      at("2026-09-11T09:00:00Z"),
			want:     false,
		},
		{
			name:     "yesterday's run, and today's time has come",
			schedule: backup.Schedule{At: "03:00", Timezone: "UTC", LastRunAt: ran("2026-09-10T03:00:00Z")},
			now:      at("2026-09-11T03:00:00Z"),
			want:     true,
		},
		{
			// Before today's occurrence, yesterday's is the one that
			// counts — and it was taken.
			name:     "the day is young and last night's was taken",
			schedule: backup.Schedule{At: "03:00", Timezone: "UTC", LastRunAt: ran("2026-09-10T03:00:00Z")},
			now:      at("2026-09-10T22:00:00Z"),
			want:     false,
		},
		{
			// The daemon was down at 03:00 and came back at 07:00. A
			// missed window runs late rather than being skipped: a
			// night with no backup is what this exists to prevent, and
			// being four hours off is not.
			name:     "the window was missed while the daemon was down",
			schedule: backup.Schedule{At: "03:00", Timezone: "UTC", LastRunAt: ran("2026-09-10T03:00:00Z")},
			now:      at("2026-09-11T07:00:00Z"),
			want:     true,
		},
		{
			// No zone at all is UTC rather than the machine's own, so
			// two instances in different datacentres read one row the
			// same way.
			name:     "an empty zone is UTC",
			schedule: backup.Schedule{At: "03:00", LastRunAt: ran("2026-09-11T03:00:05Z")},
			now:      at("2026-09-11T09:00:00Z"),
			want:     false,
		},
		{
			// A time nobody can parse must never fire. The service
			// refuses one where the person who typed it is still
			// watching, and this is the other side of that: a row that
			// got in anyway does nothing rather than something every
			// minute.
			name:     "a time that is not one",
			schedule: backup.Schedule{At: "tonight", Timezone: "UTC"},
			now:      at("2026-09-11T09:00:00Z"),
			want:     false,
		},
		{
			name:     "a zone this machine does not know",
			schedule: backup.Schedule{At: "03:00", Timezone: "Mars/Olympus"},
			now:      at("2026-09-11T09:00:00Z"),
			want:     false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backup.Due(&c.schedule, c.now); got != c.want {
				t.Errorf("Due(%+v, %s) = %v, want %v", c.schedule, c.now.Format(time.RFC3339), got, c.want)
			}
		})
	}
}

// 03:00 on a server's clock is not the middle of anybody's night, which
// is why the row carries a zone. The proof is one instant and one
// last-run read two ways: in UTC today's 03:00 has passed and is owed,
// and three hours west it has not arrived yet, so last night's is still
// the most recent one and it was taken.
func TestTheZoneDecidesWhichNightItIs(t *testing.T) {
	now := at("2026-09-11T04:00:00Z")
	lastNight := ran("2026-09-10T06:00:00Z") // 03:00 in São Paulo, on the 10th.

	inUTC := backup.Schedule{At: "03:00", Timezone: "UTC", LastRunAt: lastNight}
	if !backup.Due(&inUTC, now) {
		t.Error("04:00 UTC is past 03:00 UTC and the last run was yesterday, so one is owed")
	}

	inSaoPaulo := backup.Schedule{At: "03:00", Timezone: "America/Sao_Paulo", LastRunAt: lastNight}
	if backup.Due(&inSaoPaulo, now) {
		t.Error("04:00 UTC is 01:00 in São Paulo, and that night's backup was already taken")
	}
}

// The refusals happen where the person who typed one is still watching.
// "3:00" is accepted because Go reads it as three in the morning, which
// is what whoever typed it meant.
func TestWhatCountsAsATimeOfDay(t *testing.T) {
	accepted := map[string][2]int{
		"00:00": {0, 0},
		"03:00": {3, 0},
		"3:00":  {3, 0},
		"23:59": {23, 59},
	}
	for in, want := range accepted {
		hour, minute, err := backup.ParseTimeOfDay(in)
		if err != nil {
			t.Errorf("ParseTimeOfDay(%q): %v", in, err)
			continue
		}
		if hour != want[0] || minute != want[1] {
			t.Errorf("ParseTimeOfDay(%q) = %d:%02d, want %d:%02d", in, hour, minute, want[0], want[1])
		}
	}

	for _, in := range []string{"", "tonight", "24:00", "12:60", "0300", " 03:00", "03:00:00"} {
		if _, _, err := backup.ParseTimeOfDay(in); err == nil {
			t.Errorf("ParseTimeOfDay(%q) was accepted, and nothing downstream would ever fire on it", in)
		}
	}
}

// A key is what somebody reads in a bucket listing, so it sorts by
// itself and says which database it came from. Sorted lexically, the
// oldest is first — which is only true because the timestamp is zero-
// padded and in UTC.
func TestABackupIsNamedSoAListingSortsItself(t *testing.T) {
	pg := backup.KeyFor("pg", at("2026-09-11T02:05:09Z"))
	if pg != "cubeship/pg/2026-09-11T020509Z.dump" {
		t.Fatalf("KeyFor = %q", pg)
	}

	keys := []string{
		backup.KeyFor("pg", at("2026-09-11T03:00:00Z")),
		backup.KeyFor("pg", at("2026-09-09T03:00:00Z")),
		backup.KeyFor("pg", at("2026-09-10T23:00:00Z")),
	}
	want := append([]string(nil), keys...)
	sort.Strings(want)
	if want[0] != keys[1] || want[2] != keys[0] {
		t.Errorf("sorted keys are %v; a listing should read oldest first", want)
	}

	// Two databases never land on one key, whatever the moment.
	if backup.KeyFor("pg", at("2026-09-11T03:00:00Z")) == backup.KeyFor("pg-staging", at("2026-09-11T03:00:00Z")) {
		t.Error("two databases backed up in the same second share a key")
	}
}
