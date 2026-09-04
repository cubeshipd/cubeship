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

	// Same reasoning one level down: an app left at project_id/
	// environment_id 0 would fail resolving inherited env vars on deploy.
	project, err := s.GetProjectBySlug(ctx, org.ID, DefaultProjectSlug)
	if err != nil {
		t.Fatalf("expected a %q project to be created for adopted apps: %v", DefaultProjectSlug, err)
	}
	if apps[0].ProjectID != project.ID {
		t.Fatalf("expected the migrated app to belong to project %q (id %d), got project_id %d",
			DefaultProjectSlug, project.ID, apps[0].ProjectID)
	}
	env, err := s.GetEnvironmentBySlug(ctx, project.ID, ProductionEnvSlug)
	if err != nil {
		t.Fatalf("expected a %q environment to be created for adopted apps: %v", ProductionEnvSlug, err)
	}
	if apps[0].EnvironmentID != env.ID {
		t.Fatalf("expected the migrated app to belong to environment %q (id %d), got environment_id %d",
			ProductionEnvSlug, env.ID, apps[0].EnvironmentID)
	}
}

// A database from the organizations-but-no-projects era (apps.org_id
// exists, apps.project_id/environment_id don't) hits the same "no such
// column" crash one column later — this covers that upgrade path
// directly, without going through the org_id migration too.
func TestOpenMigratesDatabaseWithoutProjectsOrEnvironments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cubeship.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE organizations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE apps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			org_id INTEGER NOT NULL REFERENCES organizations(id),
			name TEXT NOT NULL UNIQUE,
			domain TEXT NOT NULL,
			image TEXT NOT NULL UNIQUE,
			container_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			env TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO organizations (slug, name) VALUES ('acme', 'Acme Inc');
		INSERT INTO apps (org_id, name, domain, image) VALUES (1, 'oldapp', 'old.example.com', 'registry.example.com/oldapp');`); err != nil {
		t.Fatalf("create pre-projects schema: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a pre-projects database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	app, err := s.GetAppByName(ctx, "oldapp")
	if err != nil {
		t.Fatalf("GetAppByName after migration: %v", err)
	}
	org, err := s.GetOrganizationBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("GetOrganizationBySlug: %v", err)
	}
	project, err := s.GetProjectBySlug(ctx, org.ID, DefaultProjectSlug)
	if err != nil {
		t.Fatalf("expected a %q project to be created for the adopted app: %v", DefaultProjectSlug, err)
	}
	if app.ProjectID != project.ID {
		t.Fatalf("expected the app to be adopted into project %d, got %d", project.ID, app.ProjectID)
	}
	env, err := s.GetEnvironmentBySlug(ctx, project.ID, ProductionEnvSlug)
	if err != nil {
		t.Fatalf("expected a %q environment to be created for the adopted app: %v", ProductionEnvSlug, err)
	}
	if app.EnvironmentID != env.ID {
		t.Fatalf("expected the app to be adopted into environment %d, got %d", env.ID, app.EnvironmentID)
	}
}

// apps.env was added the same no-op way one release earlier, so a
// database from before it has the identical problem.
func TestOpenMigratesDatabaseWithoutAppsEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cubeship.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE apps (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			domain TEXT NOT NULL,
			image TEXT NOT NULL UNIQUE,
			container_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO apps (name, domain, image) VALUES ('oldapp', 'old.example.com', 'registry.example.com/oldapp');`); err != nil {
		t.Fatalf("create pre-env schema: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a pre-env database: %v", err)
	}
	defer s.Close()

	app, err := s.GetAppByName(context.Background(), "oldapp")
	if err != nil {
		t.Fatalf("GetAppByName after migration: %v", err)
	}
	if len(app.Env) != 0 {
		t.Fatalf("expected an empty env map for a migrated app, got %v", app.Env)
	}
	if app.OrgID == 0 {
		t.Fatal("expected the migrated app to be adopted into an organization")
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

// A database from before api_keys.name existed hits the same "no such
// column" crash as every other column added by migrate — this covers
// api_keys specifically, including that a pre-existing key survives with
// a real name rather than an empty one (which would be indistinguishable
// from "never named" once ListAPIKeysForUser exists).
func TestOpenMigratesDatabaseWithoutAPIKeyName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cubeship.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			is_super_admin INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id),
			key_hash TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME
		);
		INSERT INTO users (username, is_super_admin) VALUES ('admin', 1);
		INSERT INTO api_keys (user_id, key_hash) VALUES (1, 'old-key-hash');`); err != nil {
		t.Fatalf("create pre-name schema: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a pre-name database: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	key, err := s.GetAPIKeyByHash(ctx, "old-key-hash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash after migration: %v", err)
	}
	if key.Name != DefaultAPIKeyName {
		t.Fatalf("expected the pre-existing key to be named %q, got %q", DefaultAPIKeyName, key.Name)
	}

	// The old key still authenticates — migration must not have touched
	// its hash or its ability to resolve to the user.
	user, err := s.GetUserByAPIKeyHash(ctx, "old-key-hash")
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash after migration: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("expected the key to still resolve to admin, got %q", user.Username)
	}
}
