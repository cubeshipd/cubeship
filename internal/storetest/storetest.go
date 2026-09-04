// Package storetest hands tests an isolated, empty Cubeship database.
//
// Postgres has no in-memory mode, so isolation comes from a schema per
// test rather than a server per test: every New creates a fresh schema in
// one shared Postgres, runs the migrations into it, and drops it when the
// test ends. Tests stay independent and can run in parallel while paying
// for one server.
package storetest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"cubeship/internal/store"

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

// New returns a Store backed by a schema of its own, dropped when t
// finishes.
//
// It fails the test — rather than skipping — when no database is
// reachable. A skip here would let `make check` go green on a machine
// with no Postgres, reporting success for tests that never ran.
func New(t testing.TB) *store.Store {
	t.Helper()
	s, _ := newInSchema(t)
	return s
}

// NewWithRawDB is New plus a second, raw connection into the same schema,
// for a test that has to reach past the store's API — injecting a
// database fault to prove a transaction rolls back, say. The raw handle
// is closed with the test.
func NewWithRawDB(t testing.TB) (*store.Store, *sql.DB) {
	t.Helper()
	s, schema := newInSchema(t)

	dsn, err := store.DSNWithSchema(DSN(), schema)
	if err != nil {
		t.Fatalf("build raw DSN: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return s, db
}

func newInSchema(t testing.TB) (*store.Store, string) {
	t.Helper()

	schema := "test_" + randomSuffix(t)
	s, err := store.OpenSchema(DSN(), schema)
	if err != nil {
		t.Fatalf("connect to the test database: %v\n\n"+
			"Cubeship's tests need a real Postgres (there is no in-memory mode).\n"+
			"Start one with:  make db-up\n"+
			"Or point them elsewhere with %s.", err, EnvDSN)
	}

	t.Cleanup(func() {
		// Drop before closing: the schema can only be dropped through a
		// connection, and this Store owns the only pool that has one.
		if err := s.DropSchema(context.Background(), schema); err != nil {
			t.Errorf("drop test schema %s: %v", schema, err)
		}
		s.Close()
	})
	return s, schema
}

func randomSuffix(t testing.TB) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	return strings.ToLower(hex.EncodeToString(b))
}
