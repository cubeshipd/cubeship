package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
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
	schema := "catalog_" + hex.EncodeToString(b)
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
		Problems: []template.Diagnostic{}, Manifest: manifest, Source: "s", Readme: "r", Icon: squarePNG(128)}
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

func TestTheStoreReadsWhatTheAPIServes(t *testing.T) {
	p := testStore(t)
	ctx := context.Background()
	manifest := template.Validate(fixture(t)).Manifest

	for i, stars := range []int{5, 9, 1} {
		r := umamiRepo()
		r.ID, r.NodeID, r.Stars = int64(100+i), fmt.Sprintf("R_%d", i), stars
		r.Name = fmt.Sprintf("cubeship-app%d-template", i)
		if err := p.SaveRepository(ctx, r, ""); err != nil {
			t.Fatal(err)
		}
		if err := p.SaveRelease(ctx, Indexed{RepositoryID: r.ID, Tag: "v1", Commit: fmt.Sprintf("c%d", i), URL: "u",
			PublishedAt: published.AddDate(0, 0, i), Accepted: true, Problems: []template.Diagnostic{},
			Manifest: manifest, Source: "src", Readme: "# app", Icon: squarePNG(128)}); err != nil {
			t.Fatal(err)
		}
	}
	// Refused only: known, not listed, and its history still readable.
	refused := umamiRepo()
	refused.ID, refused.NodeID, refused.Name = 200, "R_refused", "cubeship-broken-template"
	p.SaveRepository(ctx, refused, "")
	p.SaveRelease(ctx, Indexed{RepositoryID: 200, Tag: "v1", Commit: "x", URL: "u", PublishedAt: published,
		Problems: []template.Diagnostic{{Severity: template.Error, Code: "release.icon-missing", Path: []any{}}}})

	page, next, err := p.List(ctx, Query{Sort: "stars", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].Stars != 9 || page[1].Stars != 5 || next == "" {
		t.Fatalf("first page %+v next %q", page, next)
	}
	rest, next, err := p.List(ctx, Query{Sort: "stars", Limit: 2, Cursor: next})
	if err != nil || len(rest) != 1 || rest[0].Stars != 1 || next != "" {
		t.Fatalf("second page %+v next %q err %v", rest, next, err)
	}
	recent, _, _ := p.List(ctx, Query{Sort: "recent", Limit: 10})
	if len(recent) != 3 || recent[0].Name != "cubeship-app2-template" || recent[0].Title != "App2" || !recent[0].HasIcon {
		t.Errorf("recent %+v", recent)
	}
	if found, _, _ := p.List(ctx, Query{Q: "APP1", Limit: 10}); len(found) != 1 {
		t.Errorf("search found %d", len(found))
	}
	if found, _, _ := p.List(ctx, Query{Q: "100%", Limit: 10}); len(found) != 0 {
		t.Errorf("a %% in a search matched %d", len(found))
	}
	if found, _, _ := p.List(ctx, Query{Tag: "analytics", Limit: 10}); len(found) != 3 {
		t.Errorf("tag found %d", len(found))
	}
	if _, _, err := p.List(ctx, Query{Cursor: "!!", Limit: 1}); !errors.Is(err, ErrBadCursor) {
		t.Errorf("bad cursor: %v", err)
	}

	d, err := p.Template(ctx, "LUCASAARCH", "cubeship-app0-template")
	if err != nil || d == nil || d.Readme != "# app" || !strings.Contains(string(d.Manifest), `"project":"umami"`) ||
		d.SourceURL != "https://raw.githubusercontent.com/lucasaarch/cubeship-app0-template/c0/template.yaml" {
		t.Fatalf("template %+v err %v", d, err)
	}
	if d, _ := p.Template(ctx, "lucasaarch", "cubeship-broken-template"); d != nil {
		t.Error("a template with no accepted release is listed")
	}

	history, found, err := p.Releases(ctx, "lucasaarch", "cubeship-broken-template")
	if err != nil || !found || len(history) != 1 || history[0].Status != "rejected" || !strings.Contains(string(history[0].Problems), "release.icon-missing") {
		t.Errorf("history %+v found %v err %v", history, found, err)
	}
	if m, err := p.Manifest(ctx, "lucasaarch", "cubeship-app1-template", "v1"); err != nil || m == nil {
		t.Errorf("manifest %s %v", m, err)
	}
	if m, _ := p.Manifest(ctx, "lucasaarch", "cubeship-app1-template", "v9"); m != nil {
		t.Error("a manifest for a release that does not exist")
	}
	if tags, err := p.Tags(ctx); err != nil || !slices.Equal(tags, []string{"analytics"}) {
		t.Errorf("tags %v %v", tags, err)
	}
	if icon, err := p.Icon(ctx, 101, "c1"); err != nil || len(icon) == 0 {
		t.Errorf("icon %d bytes, %v", len(icon), err)
	}
	if icon, _ := p.Icon(ctx, 200, "x"); icon != nil {
		t.Error("an icon for a refused release")
	}
}
