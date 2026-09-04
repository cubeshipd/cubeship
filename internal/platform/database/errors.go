package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is Postgres' SQLSTATE for "this row already exists".
const uniqueViolation = "23505"

// IsUniqueViolation reports whether err is a unique-constraint failure.
//
// It exists so a service can insert and let the database decide, instead
// of asking "does this exist?" and then inserting. That check-then-insert
// pattern is a race: two callers both see nothing, both insert, and the
// loser gets a 500 built from a raw driver error. Letting the constraint
// answer is both correct under concurrency and one round trip cheaper.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}
