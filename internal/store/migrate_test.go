package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// legacySchema is the apps/deployments schema as it shipped before
// organizations existed: no apps.org_id, and no org/user tables at all.
const legacySchema = `
CREATE TABLE apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	env TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE deployments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id),
	image_ref TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

// writeLegacyDB creates a database in the pre-organizations shape with
// one already-deployed app in it, as an upgrading operator would have.
func writeLegacyDB(t *testing.T, apps ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cubeship.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	for _, name := range apps {
		if _, err := db.Exec(
			`INSERT INTO apps (name, domain, image, container_id, status) VALUES (?, ?, ?, ?, ?)`,
			name, name+".example.com", "registry.example.com/"+name, "container-1", "running"); err != nil {
			t.Fatalf("insert legacy app: %v", err)
		}
	}
	return path
}

// Opening a pre-organizations database used to succeed and then fail on
// the first apps query with "no such column: org_id", crash-looping the
// daemon under systemd.
func TestOpenMigratesPreOrganizationsDatabase(t *testing.T) {
	path := writeLegacyDB(t, "legacyapp")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a legacy database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	apps, err := s.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps after migration: %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("expected the pre-existing app to survive the migration, got %d apps", len(apps))
	}
	if apps[0].Name != "legacyapp" || apps[0].Status != "running" {
		t.Fatalf("expected the app's own data to be untouched, got %+v", apps[0])
	}

	// An app owned by org_id 0 would fail every authorization check.
	org, err := s.GetOrganizationBySlug(ctx, DefaultOrgSlug)
	if err != nil {
		t.Fatalf("expected a %q organization to be created for adopted apps: %v", DefaultOrgSlug, err)
	}
	if apps[0].OrgID != org.ID {
		t.Fatalf("expected the migrated app to belong to %q (id %d), got org_id %d",
			DefaultOrgSlug, org.ID, apps[0].OrgID)
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	path := writeLegacyDB(t, "legacyapp")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	ctx := context.Background()
	if _, err := second.ListApps(ctx); err != nil {
		t.Fatalf("ListApps after reopening: %v", err)
	}
	orgs, err := second.ListOrganizations(ctx)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("expected the second Open to reuse the default org, got %d organizations", len(orgs))
	}
}

// A database that already has apps.org_id (a legacy one with no apps in
// it, or a fresh install) must not gain a stray "default" organization.
func TestMigrationCreatesNoDefaultOrgWithoutOrphanedApps(t *testing.T) {
	empty, err := Open(writeLegacyDB(t))
	if err != nil {
		t.Fatalf("Open on an empty legacy database: %v", err)
	}
	defer empty.Close()

	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open on a fresh database: %v", err)
	}
	defer fresh.Close()

	ctx := context.Background()
	for name, s := range map[string]*Store{"upgraded-empty": empty, "fresh": fresh} {
		orgs, err := s.ListOrganizations(ctx)
		if err != nil {
			t.Fatalf("%s: ListOrganizations: %v", name, err)
		}
		if len(orgs) != 0 {
			t.Fatalf("%s: expected no organizations, got %v", name, orgs)
		}
	}
}
