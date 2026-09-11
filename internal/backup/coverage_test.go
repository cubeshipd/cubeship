package backup_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type coverageRow struct {
	Database  string `json:"database"`
	Engine    string `json:"engine"`
	CanBackUp bool   `json:"can_back_up"`
	Protected bool   `json:"protected"`
	Failing   bool   `json:"failing"`
	Count     int    `json:"count"`
	Schedule  *struct {
		At string `json:"at"`
	} `json:"schedule"`
	LastGood *backupRow `json:"last_good"`
	Last     *backupRow `json:"last"`
}

func coverage(t *testing.T, f *fixture) map[string]coverageRow {
	t.Helper()
	var rows []coverageRow
	rec := f.Do(t, http.MethodGet, "/backups/coverage", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("coverage: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode coverage: %v", err)
	}
	out := make(map[string]coverageRow, len(rows))
	for _, r := range rows {
		out[r.Database] = r
	}
	return out
}

// **The row a list of backups cannot contain.**
//
// This is the whole reason the endpoint exists. `GET /backups` answers
// "what has been taken", so a database nobody has ever dumped appears
// in it nowhere at all — and the screen built on it showed, on an
// instance where nothing was backed up, an empty table and no hint that
// anything was wrong.
func TestADatabaseWithNoBackupsIsTheFirstThingReported(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	if got := f.backups(t, ""); len(got) != 0 {
		t.Fatalf("expected no backups to exist, got %d", len(got))
	}

	rows := coverage(t, f)
	row, ok := rows["pg"]
	if !ok {
		t.Fatalf("pg is missing from the coverage report: %v", rows)
	}
	if row.Protected {
		t.Error("a database with no backups reports itself protected")
	}
	if row.Count != 0 || row.LastGood != nil || row.Last != nil {
		t.Errorf("pg has never been dumped, yet %+v", row)
	}
}

// **Protected is two facts and needs both.** A dump on this machine's
// own disk survives somebody dropping a table and nothing else — not
// the disk, not the box — so a screen calling it a backup would be the
// only thing on the instance claiming otherwise.
func TestADumpOnThisMachineIsNotBeingProtected(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.take(t, "pg")

	row := coverage(t, f)["pg"]
	if row.LastGood == nil {
		t.Fatal("the dump that just succeeded is not reported")
	}
	if row.Protected {
		t.Error("a dump on this machine's own disk reports the database protected")
	}

	// The same dump, sent somewhere else.
	f.linkStore(t, "dumps")
	f.schedule(t, "pg", map[string]any{
		"at": "03:00", "timezone": "UTC", "keep": 7, "store": "dumps", "bucket": "backups",
	})
	f.take(t, "pg")

	row = coverage(t, f)["pg"]
	if !row.Protected {
		t.Errorf("a dump in a bucket does not report the database protected: %+v", row)
	}
}

// **Failing is not the opposite of protected**, and the case that
// proves it is the one worth reporting: last week's dump sitting safely
// in a bucket while every night since has failed. Reporting only the
// good one says everything is fine; reporting only the last one says
// there is nothing to restore.
func TestADatabaseCanBeProtectedAndFailingAtOnce(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "dumps")
	f.schedule(t, "pg", map[string]any{
		"at": "03:00", "timezone": "UTC", "keep": 0, "store": "dumps", "bucket": "backups",
	})
	f.take(t, "pg")

	// And then the engine starts refusing.
	f.docker.set("", 1, "pg_dump: error: connection to server failed")
	f.take(t, "pg")

	row := coverage(t, f)["pg"]
	if !row.Protected {
		t.Error("the good dump in the bucket is no longer reported")
	}
	if !row.Failing {
		t.Error("a database whose last attempt failed does not report failing")
	}
	if row.Last == nil || row.LastGood == nil || row.Last.ID == row.LastGood.ID {
		t.Errorf("the last attempt and the last good one should be two rows: %+v", row)
	}
}

// An engine this instance does not dump is still a row. Left out, it
// would read as a database nobody had checked — which is the one thing
// a coverage report must never do.
func TestAnEngineThatIsNotBackedUpIsStillReported(t *testing.T) {
	f := newFixture(t)
	f.database(t, "cache", "redis")

	row, ok := coverage(t, f)["cache"]
	if !ok {
		t.Fatal("redis is missing from the coverage report")
	}
	if row.CanBackUp {
		t.Error("redis reports that this instance backs it up")
	}
	if row.Protected {
		t.Error("redis reports itself protected")
	}
}

// The orphans are their own listing, because they are the one kind of
// row that cannot be restored — and a table where some rows can be and
// some cannot is one somebody reads wrong.
func TestTheOrphansAreListedApartFromTheRest(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.database(t, "other", "postgres")
	f.take(t, "pg")
	f.take(t, "other")

	rec := f.Do(t, http.MethodDelete, "/datastores/pg", nil, f.AdminKey)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete pg: %d %s", rec.Code, rec.Body.String())
	}

	var orphans []backupRow
	rec = f.Do(t, http.MethodGet, "/backups/orphans", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("orphans: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &orphans); err != nil {
		t.Fatalf("decode orphans: %v", err)
	}
	if len(orphans) != 1 || orphans[0].Database != "pg" {
		t.Fatalf("want one orphan from pg, got %+v", orphans)
	}

	// And the database that is still here is not one of them.
	if _, ok := coverage(t, f)["pg"]; ok {
		t.Error("a deleted database is still in the coverage report")
	}
	if _, ok := coverage(t, f)["other"]; !ok {
		t.Error("the database that is still here is missing from the report")
	}
}
