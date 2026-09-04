package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
)

// newTestStore is internal/storetest.New, reimplemented here because
// these tests live in package store: importing storetest, which imports
// store, would be an import cycle. Keep the two in step — the mechanism
// (a schema per test, dropped on cleanup) is the same.
func newTestStore(t testing.TB) *Store {
	t.Helper()

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "test_" + hex.EncodeToString(b)

	dsn := os.Getenv("CUBESHIP_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://cubeship:cubeship@127.0.0.1:5433/cubeship_test?sslmode=disable"
	}

	s, err := OpenSchema(dsn, schema)
	if err != nil {
		t.Fatalf("connect to the test database: %v\n\n"+
			"Cubeship's tests need a real Postgres (there is no in-memory mode).\n"+
			"Start one with:  make db-up", err)
	}
	t.Cleanup(func() {
		if err := s.DropSchema(context.Background(), schema); err != nil {
			t.Errorf("drop test schema %s: %v", schema, err)
		}
		s.Close()
	})
	return s
}
