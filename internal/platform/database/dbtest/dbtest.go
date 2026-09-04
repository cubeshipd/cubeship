// Package dbtest hands tests an isolated, empty Cubeship database.
//
// Postgres has no in-memory mode, so isolation comes from a schema per
// test rather than a server per test: every New creates a fresh schema in
// one shared Postgres, migrates it, and drops it when the test ends.
// Tests stay independent and can run in parallel while paying for one
// server.
package dbtest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"testing"

	"cubeship/internal/platform/database"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// DefaultDSN is the Postgres `make db-up` starts. Override it with
// CUBESHIP_TEST_DATABASE_URL to point the tests at another server.
const DefaultDSN = "postgres://cubeship:cubeship@127.0.0.1:5433/cubeship_test?sslmode=disable"

// EnvDSN is the environment variable that overrides DefaultDSN.
const EnvDSN = "CUBESHIP_TEST_DATABASE_URL"

// DSN returns the connection string the tests should use.
func DSN() string {
	if dsn := os.Getenv(EnvDSN); dsn != "" {
		return dsn
	}
	return DefaultDSN
}

// New returns a DB backed by a schema of its own, dropped when t
// finishes.
//
// It fails the test — rather than skipping — when no database is
// reachable. A skip here would let `make check` go green on a machine
// with no Postgres, reporting success for tests that never ran.
func New(t testing.TB) *database.DB {
	t.Helper()
	db, _ := newInSchema(t)
	return db
}

// NewWithRawDB is New plus a second, raw connection into the same schema,
// for a test that has to reach past the repositories — injecting a
// database fault to prove a transaction rolls back, say. The raw handle
// is closed with the test.
func NewWithRawDB(t testing.TB) (*database.DB, *sql.DB) {
	t.Helper()
	db, schema := newInSchema(t)

	dsn, err := database.DSNWithSchema(DSN(), schema)
	if err != nil {
		t.Fatalf("build raw DSN: %v", err)
	}
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	t.Cleanup(func() { raw.Close() })
	return db, raw
}

func newInSchema(t testing.TB) (*database.DB, string) {
	t.Helper()

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b)

	db, err := database.OpenSchema(DSN(), schema)
	if err != nil {
		t.Fatalf("connect to the test database: %v\n\n"+
			"Cubeship's tests need a real Postgres (there is no in-memory mode).\n"+
			"Start one with:  make db-up\n"+
			"Or point them elsewhere with %s.", err, EnvDSN)
	}

	t.Cleanup(func() {
		// Drop before closing: the schema can only be dropped through a
		// connection, and this DB owns the only pool that has one.
		if err := db.DropSchema(context.Background(), schema); err != nil {
			t.Errorf("drop test schema %s: %v", schema, err)
		}
		db.Close()
	})
	return db, schema
}
