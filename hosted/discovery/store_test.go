package discovery

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"cubeship/template"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The same Postgres the product's tests use (`make db-up`), with a
// schema per test. Skipped under -short like every DB-backed test in
// the repository, and a failure — never a skip — without it.
func testStore(t *testing.T) *Postgres {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Postgres; runs without -short")
	}
	dsn := os.Getenv("CUBESHIP_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://cubeship:cubeship@127.0.0.1:5433/cubeship_test?sslmode=disable"
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	b := make([]byte, 6)
	rand.Read(b)
	schema := "discovery_" + hex.EncodeToString(b)
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("no Postgres at %s (make db-up): %v", dsn, err)
	}
	t.Cleanup(func() { admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	p := &Postgres{DB: db}
	if err := p.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTheStoreKeepsTheNewestAcceptedRelease(t *testing.T) {
	p := testStore(t)
	ctx := context.Background()
	repo := umamiRepo()
	if err := p.SaveRepository(ctx, repo, ""); err != nil {
		t.Fatal(err)
	}
	manifest := template.Validate(fixture(t)).Manifest

	older := Indexed{RepositoryID: 7, Tag: "v1", Commit: "a", URL: "u", PublishedAt: published, Accepted: true,
		Problems: []template.Diagnostic{}, Manifest: manifest, Source: "s", Readme: "r", IconKey: "k"}
	newer := older
	newer.Tag, newer.Commit, newer.PublishedAt = "v2", "b", published.AddDate(0, 0, 1)
	rejected := Indexed{RepositoryID: 7, Tag: "v3", Commit: "c", URL: "u", PublishedAt: published.AddDate(0, 0, 2),
		Problems: []template.Diagnostic{{Severity: template.Error, Code: "release.icon-missing", Path: []any{}}}}
	for _, r := range []Indexed{older, newer, rejected, newer} {
		if err := p.SaveRelease(ctx, r); err != nil {
			t.Fatal(err)
		}
	}

	var latest bool
	var count int
	if err := p.DB.QueryRowContext(ctx, `
		SELECT r.tag = 'v2', (SELECT count(*) FROM releases)
		FROM repositories p JOIN releases r ON r.id = p.latest_release_id`).Scan(&latest, &count); err != nil {
		t.Fatal(err)
	}
	if !latest || count != 3 {
		t.Errorf("latest is v2: %v, releases: %d", latest, count)
	}
	if done, err := p.Indexed(ctx, 7, "v3", "c"); err != nil || !done {
		t.Errorf("v3 indexed = %v, %v", done, err)
	}

	if _, err := p.DB.ExecContext(ctx, `INSERT INTO blocklist (subject) VALUES ('LucasAarch')`); err != nil {
		t.Fatal(err)
	}
	blocked, err := p.Blocked(ctx)
	if err != nil || !blocked["lucasaarch"] {
		t.Errorf("blocked = %v, %v", blocked, err)
	}
	if err := p.Hide(ctx, 7, HiddenGone); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveRepository(ctx, repo, ""); err != nil {
		t.Fatal(err)
	}
	var hidden sql.NullString
	p.DB.QueryRowContext(ctx, `SELECT hidden FROM repositories WHERE id = 7`).Scan(&hidden)
	if hidden.Valid {
		t.Errorf("a repository seen again is still hidden: %q", hidden.String)
	}

	ran, err := p.Locked(ctx, func() error {
		again, err := p.Locked(ctx, func() error { return nil })
		if again || err != nil {
			t.Errorf("the lock was taken twice: %v, %v", again, err)
		}
		return nil
	})
	if !ran || err != nil {
		t.Errorf("locked = %v, %v", ran, err)
	}
}
