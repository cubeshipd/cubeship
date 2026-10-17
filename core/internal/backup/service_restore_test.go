package backup_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"cubeship/internal/backup"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/user"
)

// With nothing linked, a backup still happens — on this machine's own
// disk, which is not a backup and says so. That is deliberate: an
// instance that could take none at all until somebody had linked a
// bucket is worse.
func TestWithNothingLinkedABackupStillHappensAndSaysWhereItIs(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	row := f.take(t, "pg")
	if row.Status != "succeeded" {
		t.Fatalf("the backup came back %q: %s", row.Status, row.Error)
	}
	if row.OffMachine || row.Store != "" {
		t.Errorf("a dump on this machine's own disk reports off_machine=%v store=%q",
			row.OffMachine, row.Store)
	}

	rec := f.Do(t, http.MethodGet, "/backups/"+itoa(row.ID)+"/download", nil, f.AdminKey)
	if rec.Code != http.StatusOK || rec.Body.String() != "-- a dump\n" {
		t.Fatalf("download from the local disk: %d %q", rec.Code, rec.Body.String())
	}
}

// A dump the engine refused is a failed backup, not a failed request —
// and the row is what says so. It is kept, because a backup that did
// not happen is the thing somebody most needs to find out about, and
// what the engine said is carried into it: "exit status 1" is not a
// reason anybody can act on.
//
// Nothing is left in the bucket. A half-uploaded dump left behind is
// one somebody restores from.
func TestADumpThatFailsKeepsTheRowAndLeavesNothingBehind(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "keep": 7, "store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("set a schedule: %d", code)
	}
	f.docker.set("", 1, "pg_dump: error: connection to server failed")

	row := f.take(t, "pg")
	if row.Status != "failed" {
		t.Fatalf("a dump the engine refused came back %q", row.Status)
	}
	if !strings.Contains(row.Error, "connection to server failed") {
		t.Errorf("the row says %q, and what the engine said is the only reason anybody can act on", row.Error)
	}
	if keys := f.store.keys("dumps"); len(keys) != 0 {
		t.Errorf("the bucket holds %v after a failed dump", keys)
	}

	// Nothing complete is at the other end, so there is nothing to load
	// back — said as a refusal rather than as a half-replaced database.
	if rec := f.Do(t, http.MethodPost, "/backups/"+itoa(row.ID)+"/restore", nil, f.AdminKey); rec.Code != http.StatusConflict {
		t.Errorf("restoring from a failed backup answered %d, want 409", rec.Code)
	}
}

// An empty dump is never right — every engine writes a header even for
// an empty database — and it is the shape a silent failure takes. A
// backup that says it failed beats one that restores to nothing.
func TestAnEmptyDumpIsAFailureRatherThanAnEmptyBackup(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.docker.set("", 0, "")

	row := f.take(t, "pg")
	if row.Status != "failed" {
		t.Fatalf("a dump of nothing at all came back %q, size %d", row.Status, row.Size)
	}
}

// Deleting a database is exactly the moment its backups matter, so they
// outlive it — which only works because the name, the engine and the
// version are written down rather than joined.
//
// What cannot be done is restoring one: where it should go is a choice
// this release does not offer, and picking one would be the wrong
// database quietly replaced.
func TestABackupOutlivesTheDatabaseItCameFrom(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	row := f.take(t, "pg")

	if rec := f.Do(t, http.MethodDelete, "/datastores/pg", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Fatalf("delete the database: %d %s", rec.Code, rec.Body.String())
	}

	rows := f.backups(t, "")
	if len(rows) != 1 {
		t.Fatalf("the instance holds %d backups after its database went, want 1", len(rows))
	}
	orphan := rows[0]
	if orphan.ID != row.ID {
		t.Fatalf("the backup that survived is #%d, not the one taken", orphan.ID)
	}
	if orphan.Exists {
		t.Error("the row still claims its database is here")
	}
	if orphan.Database != "pg" || orphan.Engine != "postgres" || orphan.Version == "" {
		t.Errorf("the orphan cannot say what produced it: %+v", orphan)
	}

	if rec := f.Do(t, http.MethodPost, "/backups/"+itoa(orphan.ID)+"/restore", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("restoring an orphan answered %d, want 404", rec.Code)
	}
	// It can still be fetched, which is the point of keeping it.
	if rec := f.Do(t, http.MethodGet, "/backups/"+itoa(orphan.ID)+"/download", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Errorf("downloading an orphan answered %d", rec.Code)
	}
}

// The rule retention exists to keep: **a run of failures must not push
// the last good dump out of the window.** Counted naively — newest
// three rows, whatever they are — two failed nights are enough to make
// the only restorable backup on the instance expire, at exactly the
// moment somebody is going to need it.
func TestARunOfFailuresNeverPushesOutTheLastGoodDump(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "keep": 2, "store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("set a schedule: %d", code)
	}

	good := f.take(t, "pg")
	f.docker.set("", 1, "pg_dump: error: could not connect")
	f.take(t, "pg")
	f.take(t, "pg")

	rows := f.backups(t, "pg")
	if len(rows) != 3 {
		t.Fatalf("after one good night and two bad ones there are %d rows, want 3", len(rows))
	}
	if !held(rows, good.ID) {
		t.Fatal("two failures expired the only backup on this instance that can be restored")
	}

	// Two good nights later there is something to replace it with, and
	// only then does it go — object and row together.
	f.docker.set("-- a dump\n", 0, "")
	f.take(t, "pg")
	f.take(t, "pg")

	rows = f.backups(t, "pg")
	if held(rows, good.ID) {
		t.Error("keep=2 with three successes still holds the oldest")
	}
	if failures := countStatus(rows, "failed"); failures != 2 {
		t.Errorf("%d failed rows survived, want 2 — a failure is the evidence a schedule is not working", failures)
	}
	for _, key := range f.store.keys("dumps") {
		if key == good.Key {
			t.Error("the row was pruned and the object was left in the bucket, paid for and pointed at by nothing")
		}
	}
}

// A dump in flight is the one state a backup cannot be restored from or
// deleted in: something is still writing to it, and to the object it
// names.
//
// The row is written by hand because the only way to see this through
// the API is to catch a real dump mid-flight, which is a race a test
// should not be built on.
func TestADumpInFlightCannotBeRestoredOrDeleted(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	row, err := f.Server.Backups.Repo().Start(context.Background(), &backup.Backup{
		DatastoreID: f.datastoreID(t, "pg"), DatastoreName: "pg",
		Engine: "postgres", Version: "18",
		Key: backup.KeyFor("pg", time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if row.Status != "taking" {
		t.Fatalf("a row written before the work starts says %q", row.Status)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/backups/" + itoa(row.ID) + "/restore"},
		{http.MethodDelete, "/backups/" + itoa(row.ID)},
	} {
		if rec := f.Do(t, c.method, c.path, nil, f.AdminKey); rec.Code != http.StatusConflict {
			t.Errorf("%s %s while the dump is in flight answered %d, want 409", c.method, c.path, rec.Code)
		}
	}
}

// A dump is not loaded into an engine that did not produce it.
// `pg_dump` output is not MySQL's, and a major version apart is a
// restore that fails partway with the database already half replaced.
func TestADumpIsNotLoadedIntoAnEngineThatDidNotProduceIt(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	// Written by hand: an engine is fixed for the life of a datastore,
	// so the only rows that can disagree are ones that outlived a
	// change — which is exactly what this refusal is for.
	row, err := f.Server.Backups.Repo().Start(context.Background(), &backup.Backup{
		DatastoreID: f.datastoreID(t, "pg"), DatastoreName: "pg",
		Engine: "mysql", Version: "8.4",
		Key: backup.KeyFor("pg", time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if err := f.Server.Backups.Repo().Finish(context.Background(), row.ID, 10, ""); err != nil {
		t.Fatalf("finish the row: %v", err)
	}

	rec := f.Do(t, http.MethodPost, "/backups/"+itoa(row.ID)+"/restore", nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("restoring a MySQL dump into Postgres answered %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mysql") {
		t.Errorf("the refusal does not say what the two are: %q", rec.Body.String())
	}
	if loaded := f.docker.loaded(); len(loaded) != 0 {
		t.Error("the refusal came after the engine had already been fed")
	}
}

// Redis is not backed up, and that is a decision rather than a gap. It
// is refused where somebody can read the reason, rather than offered a
// button that does nothing.
func TestAnEngineWithNoDumpIsRefusedWithTheReason(t *testing.T) {
	f := newFixture(t)
	f.database(t, "cache", "redis")

	rec := f.Do(t, http.MethodPost, "/datastores/cache/backups", nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("backing up Redis answered %d, want 409", rec.Code)
	}
	if code := f.schedule(t, "cache", map[string]any{"at": "03:00", "keep": 7}); code != http.StatusConflict {
		t.Errorf("scheduling Redis answered %d, want 409", code)
	}
}

// Every refusal a schedule can carry happens while the person who typed
// it is still watching, rather than at three in the morning in a log
// nobody reads.
func TestAScheduleIsRefusedBeforeTheTimerCanFailOnIt(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")

	refused := map[string]map[string]any{
		"a time that is not one":         {"at": "tonight"},
		"a time out of range":            {"at": "25:00"},
		"a zone this machine lacks":      {"at": "03:00", "timezone": "Mars/Olympus"},
		"a negative count":               {"at": "03:00", "keep": -1},
		"a count nobody meant":           {"at": "03:00", "keep": 100000},
		"a store with no bucket named":   {"at": "03:00", "store": "offsite"},
		"a store this instance does not": {"at": "03:00", "store": "nowhere", "bucket": "dumps"},
	}
	for name, body := range refused {
		code := f.schedule(t, "pg", body)
		if code == http.StatusOK {
			t.Errorf("%s was accepted", name)
		}
	}

	// A good one round-trips, and every surface names the store the way
	// the rest of this product does: by its name, never by its id.
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "timezone": "America/Sao_Paulo", "keep": 7,
		"store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("a good schedule answered %d", code)
	}

	var got struct {
		At       string `json:"at"`
		Timezone string `json:"timezone"`
		Keep     int    `json:"keep"`
		Store    string `json:"store"`
		Bucket   string `json:"bucket"`
	}
	rec := f.Do(t, http.MethodGet, "/datastores/pg/backups/schedule", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read the schedule: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode the schedule: %v", err)
	}
	if got.At != "03:00" || got.Timezone != "America/Sao_Paulo" || got.Keep != 7 {
		t.Errorf("the schedule came back %+v", got)
	}
	if got.Store != "offsite" || got.Bucket != "dumps" {
		t.Errorf("the schedule names the store as %q/%q", got.Store, got.Bucket)
	}

	// Off is the row going, because the row existing is what scheduled
	// means. There is no flag that could say off with a time beside it.
	if rec := f.Do(t, http.MethodDelete, "/datastores/pg/backups/schedule", nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("unset the schedule: %d", rec.Code)
	}
	if rec := f.Do(t, http.MethodGet, "/datastores/pg/backups/schedule", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("a database with no schedule answered %d, want 404", rec.Code)
	}
}

// All of it is an admin's, reads included. A dump is every row in the
// database, so somebody who may take one may read everything it holds,
// and a restore replaces all of it — the line `objectstore` already
// draws around a bucket's contents.
func TestOnlyAnAdminMayReachABackup(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	row := f.take(t, "pg")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	id := itoa(row.ID)
	closed := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/backups", nil},
		{http.MethodGet, "/datastores/pg/backups", nil},
		{http.MethodPost, "/datastores/pg/backups", nil},
		{http.MethodGet, "/datastores/pg/backups/schedule", nil},
		{http.MethodPut, "/datastores/pg/backups/schedule", map[string]any{"at": "03:00"}},
		{http.MethodDelete, "/datastores/pg/backups/schedule", nil},
		{http.MethodPost, "/backups/" + id + "/restore", nil},
		{http.MethodGet, "/backups/" + id + "/download", nil},
		{http.MethodDelete, "/backups/" + id, nil},
	}
	for _, c := range closed {
		rec := f.Do(t, c.method, c.path, c.body, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

// --- small helpers ---

func held(rows []backupRow, id int64) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func countStatus(rows []backupRow, status string) int {
	n := 0
	for _, row := range rows {
		if row.Status == status {
			n++
		}
	}
	return n
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func (*fakeDocker) PathOwner(context.Context, string, string) (dockerx.Owner, error) {
	return dockerx.Owner{Mode: 0o755}, nil
}
