package store

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

type Store struct {
	db *sql.DB
}

// ErrNotFound is what the store's Get* methods wrap when no row matches,
// so callers can tell "no such row" from a real database failure without
// importing database/sql themselves.
var ErrNotFound = sql.ErrNoRows

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
func Open(dsn string) (*Store, error) {
	return OpenSchema(dsn, "")
}

// OpenSchema is Open confined to one Postgres schema, which it creates if
// it doesn't exist. It exists so several independent Cubeship databases
// can share one Postgres server — which is how the tests get an isolated
// database each without paying for a server per test.
//
// An empty schema means "whatever the connection's default search_path
// says", i.e. public.
func OpenSchema(dsn, schema string) (*Store, error) {
	if schema != "" && !schemaNamePattern.MatchString(schema) {
		return nil, fmt.Errorf("invalid schema name %q", schema)
	}

	connStr, err := DSNWithSchema(dsn, schema)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	if schema != "" {
		// CREATE SCHEMA needs no search_path of its own, so this works
		// even though every later statement resolves through one that
		// doesn't exist yet.
		if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quoteIdent(schema)); err != nil {
			db.Close()
			return nil, fmt.Errorf("create schema %s: %w", schema, err)
		}
	}

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
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
// It is exported so a caller that needs its own connection into a Store's
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

// DropSchema removes a schema and everything in it. Only meaningful for
// a Store opened with OpenSchema; it is how a test tears its own
// database down.
func (s *Store) DropSchema(ctx context.Context, schema string) error {
	if !schemaNamePattern.MatchString(schema) {
		return fmt.Errorf("invalid schema name %q", schema)
	}
	if _, err := s.db.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+quoteIdent(schema)+` CASCADE`); err != nil {
		return fmt.Errorf("drop schema %s: %w", schema, err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
