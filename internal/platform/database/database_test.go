package database_test

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"cubeship/internal/platform/database"
	"cubeship/internal/platform/database/dbtest"
)

// The daemon runs migrations on every start, so a second start against an
// already-migrated database must be a no-op rather than an error.
func TestMigrateIsIdempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	// dbtest.New already migrated once. Do it again on the same schema.
	if err := database.Migrate(ctx, db.DB); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	want, err := database.MigrationCount()
	if err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	var got int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0`).Scan(&got); err != nil {
		t.Fatalf("read applied migrations: %v", err)
	}
	if got != want {
		t.Errorf("%d migrations applied after migrating twice, want %d", got, want)
	}
}

// Schema-per-test is what makes the whole suite safe to run in parallel
// against one server. If it leaked, tests would see each other's rows.
func TestSchemasAreIsolated(t *testing.T) {
	ctx := context.Background()
	a := dbtest.New(t)
	b := dbtest.New(t)

	if _, err := a.ExecContext(ctx,
		`INSERT INTO organizations (slug, name) VALUES ('acme', 'Acme')`); err != nil {
		t.Fatalf("insert into schema a: %v", err)
	}

	var n int
	if err := b.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&n); err != nil {
		t.Fatalf("count in schema b: %v", err)
	}
	if n != 0 {
		t.Fatalf("schema b sees %d rows written to schema a", n)
	}
}

// WithTx must leave nothing behind when fn fails — the property every
// multi-step write in the codebase relies on.
func TestWithTxRollsBackOnError(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	sentinel := context.Canceled
	err := db.WithTx(ctx, func(tx database.Queryer) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO organizations (slug, name) VALUES ('acme', 'Acme')`); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("WithTx returned %v, want the callback's own error", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a rolled-back transaction left %d rows behind", n)
	}
}

func TestDSNWithSchema(t *testing.T) {
	t.Run("empty schema is left alone", func(t *testing.T) {
		const dsn = "postgres://u:p@host:5432/db?sslmode=disable"
		got, err := database.DSNWithSchema(dsn, "")
		if err != nil || got != dsn {
			t.Fatalf("got %q (%v), want it unchanged", got, err)
		}
	})

	t.Run("key/value DSN gets a trailing parameter", func(t *testing.T) {
		got, err := database.DSNWithSchema("host=/var/run/postgresql dbname=cubeship", "test_abc")
		if err != nil {
			t.Fatal(err)
		}
		if want := "host=/var/run/postgresql dbname=cubeship search_path=test_abc"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("URL DSN keeps its existing parameters", func(t *testing.T) {
		got, err := database.DSNWithSchema("postgres://u:p@host:5432/db?sslmode=disable", "test_abc")
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("result is not a valid URL: %v", err)
		}
		if u.Query().Get("search_path") != "test_abc" {
			t.Errorf("search_path is %q, want test_abc", u.Query().Get("search_path"))
		}
		if u.Query().Get("sslmode") != "disable" {
			t.Errorf("sslmode was dropped: %q", got)
		}
	})
}

// A schema name is an identifier, so it cannot be a bound parameter and
// has to be interpolated. Rejecting anything but a plain identifier is
// what keeps that safe.
func TestOpenSchemaRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{
		`public"; DROP TABLE users; --`,
		"has space",
		"Uppercase",
		"1leading-digit",
	} {
		// Rejection happens before any connection is attempted, so this
		// needs no database.
		_, err := database.OpenSchema("postgres://ignored/db", name)
		if err == nil {
			t.Errorf("OpenSchema accepted the schema name %q", name)
		} else if !strings.Contains(err.Error(), "invalid schema name") {
			t.Errorf("schema %q: got %v, want an invalid-schema-name error", name, err)
		}
	}
}
