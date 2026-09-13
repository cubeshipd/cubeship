package database

import (
	"context"
	"fmt"
)

// MergeJSONBMap merges setJSON into a JSONB column on one row and removes
// the unset keys from it, in a single statement.
//
// One statement, not a read-modify-write, is the whole point: two callers
// setting different keys at the same time would otherwise each write back
// a map built from the value they read, and whoever committed second
// would silently drop the other's key.
//
// table and column are interpolated because they are identifiers, not
// values, and cannot be bound. Both are always code constants — never
// anything a request supplies.
func MergeJSONBMap(ctx context.Context, q Queryer, table, column string, id int64, setJSON string, unset []string) error {
	if unset == nil {
		// A nil slice would reach Postgres as NULL, and `jsonb - NULL` is
		// NULL — which would blank the column instead of leaving it be.
		unset = []string{}
	}
	stmt := fmt.Sprintf(
		`UPDATE %s SET %s = (%s - $1::text[]) || $2::jsonb WHERE id = $3`,
		quoteIdent(table), quoteIdent(column), quoteIdent(column))
	if _, err := q.ExecContext(ctx, stmt, unset, setJSON, id); err != nil {
		return fmt.Errorf("merge %s.%s: %w", table, column, err)
	}
	return nil
}
