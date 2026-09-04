package store

import (
	"context"
	"testing"
)

// TestMigrateIsIdempotent is the property every deploy depends on: the
// daemon runs migrate on every single start, so a second start against an
// already-migrated database must be a no-op rather than an error (or, far
// worse, a re-application).
func TestMigrateIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// newTestStore already migrated once. Do it again on the same schema.
	if err := migrate(ctx, s.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}
	if n != len(migrations) {
		t.Errorf("schema_migrations has %d rows after migrating twice, want %d", n, len(migrations))
	}
}

func TestMigrateRecordsEveryVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()

	var got []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(got) != len(migrations) {
		t.Fatalf("applied %d migrations, declared %d", len(got), len(migrations))
	}
	for i, m := range migrations {
		if got[i] != m.version {
			t.Errorf("migration %d: recorded version %d, want %d", i, got[i], m.version)
		}
	}
}

// TestMigrationVersionsAreOrderedAndGapless guards the list itself: a
// duplicate or out-of-order version would make migrate skip a step on
// some databases and not others, depending on which had run before.
func TestMigrationVersionsAreOrderedAndGapless(t *testing.T) {
	for i, m := range migrations {
		if want := i + 1; m.version != want {
			t.Errorf("migrations[%d] has version %d, want %d", i, m.version, want)
		}
		if m.name == "" {
			t.Errorf("migrations[%d] (version %d) has no name", i, m.version)
		}
	}
}

// TestSchemaIsUsableAfterMigrate walks one row through the whole
// hierarchy, which is the cheapest way to catch a column the schema
// declares differently from what the queries expect.
func TestSchemaIsUsableAfterMigrate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	org, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	project, env, err := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if env.Slug != ProductionEnvSlug {
		t.Errorf("default environment is %q, want %q", env.Slug, ProductionEnvSlug)
	}
	app, err := s.CreateApp(ctx, org.ID, project.ID, env.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if app.Status != "pending" {
		t.Errorf("new app status is %q, want %q", app.Status, "pending")
	}
	if len(app.Env) != 0 {
		t.Errorf("new app env is %v, want empty", app.Env)
	}
	if app.CreatedAt.IsZero() {
		t.Error("new app has a zero created_at")
	}
}

// TestOpenSchemaIsolatesDatabases is the property the test helper rests
// on: two schemas in the same Postgres must not see each other's rows.
func TestOpenSchemaIsolatesDatabases(t *testing.T) {
	ctx := context.Background()
	a := newTestStore(t)
	b := newTestStore(t)

	if _, err := a.CreateOrganization(ctx, "acme", "Acme Inc"); err != nil {
		t.Fatalf("create organization in a: %v", err)
	}
	if _, err := b.GetOrganizationBySlug(ctx, "acme"); err == nil {
		t.Error("schema b sees an organization created in schema a")
	}
}
