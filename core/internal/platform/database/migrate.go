package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// migrationsFS carries the .sql files into the binary, so a deployed
// cubeshipd needs nothing on disk to migrate — there is no directory for
// an operator to forget to ship, and the migrations that run are exactly
// the ones the binary was built from.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// Migrate applies every migration the database hasn't seen yet.
//
// goose records what it has applied in its own table, so this is safe to
// run on every daemon start — which is when it runs, since a binary and
// its schema ship together and an operator should never have to
// remember a separate step.
//
// The provider API is used rather than goose's package-level functions
// because those keep global state (dialect, filesystem); tests open
// several databases at once, in parallel.
func Migrate(ctx context.Context, db *sql.DB) error {
	// goose reads from the root of the filesystem it is given, so the
	// embedded tree has to be rooted at the migrations directory itself.
	fsys, err := fs.Sub(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		// goose logs every applied migration to stdout by default, which
		// on a fresh database buries the daemon's own startup lines.
		goose.WithLogger(silentLogger{}))
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrationCount reports how many migrations the binary carries. Tests
// use it to assert the database ended up at the version the code expects.
func MigrationCount() (int, error) {
	entries, err := migrationsFS.ReadDir(migrationsDir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

type silentLogger struct{}

func (silentLogger) Printf(string, ...any) {}
func (silentLogger) Fatalf(format string, v ...any) {
	panic(fmt.Sprintf(format, v...))
}
