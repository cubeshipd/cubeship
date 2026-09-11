package backup_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// ownPostgres stands in for this daemon's own database.
type ownPostgres struct {
	owned bool
	dump  string
}

func (o *ownPostgres) OwnsDatabase() bool { return o.owned }
func (o *ownPostgres) Version() string    { return "16" }
func (o *ownPostgres) DumpDatabase(_ context.Context, w io.Writer) (string, error) {
	_, err := io.WriteString(w, o.dump)
	return "", err
}

// **An instance that does not run its own database cannot back itself
// up**, and says so rather than writing an archive with a hole where
// the database should be.
//
// It is the ordinary state of a server pointed at somebody else's
// Postgres with CUBESHIP_DATABASE_URL — and of every test, which is why
// the refusal is what the fixture answers until one is wired.
func TestAnInstanceWithNoDatabaseOfItsOwnSaysSo(t *testing.T) {
	f := newFixture(t)

	rec := f.Do(t, http.MethodPost, "/instance/backups", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)

	f.Server.Backups.SetInstance(&ownPostgres{owned: false})
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/instance/backups", nil, f.AdminKey),
		http.StatusConflict)
}

// **What goes in is what cannot be worked out again.** The database,
// the certificate store and the project pictures — and nothing from the
// two directories that hold everybody's actual data, which have backups
// of their own and would make the one artifact small enough to take
// nightly the one too big to take at all.
func TestTheArchiveIsTheDatabaseAndTheFilesNothingElseHasACopyOf(t *testing.T) {
	f := newFixture(t)
	f.Server.Backups.SetInstance(&ownPostgres{owned: true, dump: "-- cubeship\nCREATE TABLE t;\n"})

	dir := f.DataDir
	write(t, filepath.Join(dir, "letsencrypt", "acme.json"), `{"certificates":[]}`)
	write(t, filepath.Join(dir, "projects", "1"), "a picture")
	// The two that are deliberately out: somebody's database on disk,
	// and a managed store's objects.
	write(t, filepath.Join(dir, "datastores", "1", "base", "1", "2"), "postgres pages")
	write(t, filepath.Join(dir, "objectstores", "1", "uploads", "photo.jpg"), "a photo")

	var row backupRow
	rec := f.DoJSON(t, http.MethodPost, "/instance/backups", nil, f.AdminKey, &row)
	servertest.RequireStatus(t, rec, http.StatusAccepted)
	f.Server.Backups.Wait()

	rows := instanceBackups(t, f)
	if len(rows) != 1 || rows[0].Status != "succeeded" {
		t.Fatalf("the backup did not succeed: %+v", rows)
	}

	names := entries(t, filepath.Join(dir, "backups", "cubeship", filepath.Base(rows[0].Key)))
	want := []string{"cubeship.sql", "letsencrypt/acme.json", "projects/1"}
	if len(names) != len(want) {
		t.Fatalf("the archive holds %v, want %v", names, want)
	}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("entry %d is %q, want %q", i, names[i], name)
		}
	}
}

// **Retention counts one kind at a time.** The instance's backups and a
// database's share a table and share nothing else: without that, a
// nightly copy of the instance would push somebody's database out of
// its own window of seven, which is the one thing retention must never
// be the cause of.
func TestTheInstancesBackupsDoNotCrowdOutADatabases(t *testing.T) {
	f := newFixture(t)
	f.Server.Backups.SetInstance(&ownPostgres{owned: true, dump: "-- cubeship\n"})
	f.database(t, "pg", "postgres")
	f.schedule(t, "pg", map[string]any{"at": "03:00", "timezone": "UTC", "keep": 1})
	f.take(t, "pg")
	f.take(t, "pg")

	// Two of the instance's, with a window of one of their own.
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/instance/backups/schedule",
		map[string]any{"at": "03:00", "timezone": "UTC", "keep": 1}, f.AdminKey), http.StatusOK)
	for range 2 {
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/instance/backups", nil, f.AdminKey),
			http.StatusAccepted)
		f.Server.Backups.Wait()
	}

	if got := len(instanceBackups(t, f)); got != 1 {
		t.Errorf("the instance kept %d backups, want 1", got)
	}
	if got := len(f.backups(t, "pg")); got != 1 {
		t.Errorf("pg kept %d backups, want 1 — the instance's pruning reached into them", got)
	}
}

// Backing the instance up is reading every row in it, so it is an
// admin's — the same line a database's dump draws.
func TestOnlyAnAdminBacksTheInstanceUp(t *testing.T) {
	f := newFixture(t)
	f.Server.Backups.SetInstance(&ownPostgres{owned: true, dump: "-- cubeship\n"})
	_, memberKey := f.AddMember(t, "employee", user.RoleMember)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/instance/backups"},
		{http.MethodPost, "/instance/backups"},
		{http.MethodPut, "/instance/backups/schedule"},
		{http.MethodDelete, "/instance/backups/schedule"},
	} {
		servertest.RequireStatus(t,
			f.Do(t, c.method, c.path, map[string]any{"at": "03:00"}, memberKey),
			http.StatusForbidden)
	}
}

func instanceBackups(t *testing.T, f *fixture) []backupRow {
	t.Helper()
	var rows []backupRow
	rec := f.Do(t, http.MethodGet, "/instance/backups", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return rows
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// entries reads back what is in the archive, sorted, so the assertion
// is about what went in rather than about the order a walk happened to
// produce.
func entries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open the archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("the archive is not gzip: %v", err)
	}
	var names []string
	r := tar.NewReader(gz)
	for {
		head, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read the archive: %v", err)
		}
		names = append(names, head.Name)
	}
	sort.Strings(names)
	return names
}
