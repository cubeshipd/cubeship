// Package database owns the connection to Postgres and the schema that
// lives in it. Domain modules build their repositories on the Queryer it
// exposes; nothing outside this package touches a driver.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ErrNotFound is what a repository's lookup wraps when no row matches, so
// callers can tell "no such row" from a real database failure without
// importing database/sql themselves.
var ErrNotFound = sql.ErrNoRows

// Queryer is the subset of *sql.DB and *sql.Tx a repository uses. Every
// repository is built over it, so the same code runs either directly on
// the pool or inside a transaction.
type Queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// DB is a live connection pool with its schema already migrated. It
// embeds *sql.DB, so it is itself a Queryer.
type DB struct {
	*sql.DB
}

// Connection pool limits. The daemon serves one VPS, so a large pool buys
// nothing and a Postgres running alongside it in a container has a modest
// max_connections; the lifetime bound keeps a pooled connection from
// outliving a database restart.
const (
	maxOpenConns    = 16
	maxIdleConns    = 8
	connMaxLifetime = time.Hour
)

// Open connects to the Postgres database at dsn and brings its schema up
// to date. dsn is a libpq URL or key/value string, e.g.
// "postgres://cubeship:secret@127.0.0.1:5432/cubeship?sslmode=disable".
//
// The connection is verified before returning: database/sql connects
// lazily, so without a ping a daemon with an unreachable database starts
// happily and fails every request instead of refusing to boot.
func Open(dsn string) (*DB, error) {
	return OpenSchema(dsn, "")
}

// OpenSchema is Open confined to one Postgres schema, which it creates if
// it doesn't exist. It exists so several independent Cubeship databases
// can share one Postgres server — which is how the tests get an isolated
// database each without paying for a server per test.
//
// An empty schema means "whatever the connection's default search_path
// says", i.e. public.
func OpenSchema(dsn, schema string) (*DB, error) {
	if schema != "" && !schemaNamePattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid schema name %q", schema)
	}

	connStr, err := DSNWithSchema(dsn, schema)
	if err != nil {
		return nil, err
	}
	pool, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool.SetMaxOpenConns(maxOpenConns)
	pool.SetMaxIdleConns(maxIdleConns)
	pool.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if schema != "" {
		// CREATE SCHEMA needs no search_path of its own, so this works
		// even though every later statement resolves through one that
		// doesn't exist yet.
		if _, err := pool.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent(schema)); err != nil {
			pool.Close()
			return nil, fmt.Errorf("create schema %s: %w", schema, err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{DB: pool}, nil
}

// WithTx runs fn inside one transaction, committing when fn returns nil
// and rolling the whole thing back otherwise. It exists so a multi-step
// write (create a user, add their membership, issue their key) either
// lands completely or leaves no trace — a half-created user whose
// username is taken but who has no key and no membership is unrecoverable
// through the API.
//
// Everything fn does must go through the Queryer it is handed: a call
// back into the parent DB takes a separate connection, so it neither sees
// the transaction's uncommitted rows nor gets rolled back with it.
func (d *DB) WithTx(ctx context.Context, fn func(Queryer) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		// Covers both a returned error and fn panicking: either way,
		// nothing partial should remain visible, and the connection must
		// go back to the pool rather than leak.
		if !committed {
			tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		// The caller's error is what matters; a rollback failure on top
		// of it would only obscure the cause.
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// DropSchema removes a schema and everything in it. Only meaningful for a
// DB opened with OpenSchema; it is how a test tears its own database down.
func (d *DB) DropSchema(ctx context.Context, schema string) error {
	if !schemaNamePattern.MatchString(schema) {
		return fmt.Errorf("invalid schema name %q", schema)
	}
	if _, err := d.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`); err != nil {
		return fmt.Errorf("drop schema %s: %w", schema, err)
	}
	return nil
}

// schemaNamePattern is what OpenSchema accepts. Schema names cannot be
// passed as bound parameters — they are identifiers, not values — so the
// only safe way to interpolate one is to reject anything that isn't a
// plain lowercase identifier first.
var schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// quoteIdent wraps a SQL identifier in double quotes. Only ever called
// with a string schemaNamePattern already accepted.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// DSNWithSchema returns dsn with search_path forced to schema. Postgres
// takes search_path as a connection runtime parameter, which is the only
// pool-safe way to set it: a "SET search_path" statement would apply to
// whichever pooled connection happened to run it and to no other.
//
// It is exported so a caller that needs its own connection into a DB's
// schema — a test injecting a database fault, say — can build the same
// connection string this package would.
func DSNWithSchema(dsn, schema string) (string, error) {
	if schema == "" {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		// Not a URL — a key/value DSN ("host=... dbname=..."), where
		// parameters are appended as space-separated pairs.
		return dsn + " search_path=" + schema, nil
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
