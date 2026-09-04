package store

import (
	"context"
	"database/sql"
	"fmt"
)

// queryer is the subset of *sql.DB and *sql.Tx the store's statements
// use. Every statement is written against it once, so the same query can
// run either directly on the pool or inside a transaction.
type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Tx is a Store scoped to a single open transaction. It exposes only the
// operations that need to be grouped atomically; everything else stays on
// Store.
type Tx struct {
	q queryer
}

// WithTx runs fn inside one transaction, committing when fn returns nil
// and rolling the whole thing back otherwise. It exists so a multi-step
// write (create a user, add their membership, issue their key) either
// lands completely or leaves no trace — a half-created user whose
// username is taken but who has no key and no membership is unrecoverable
// through the API.
//
// Everything fn does must go through the *Tx it is handed: a call back
// into the parent Store takes a separate connection, so it neither sees
// the transaction's uncommitted rows nor gets rolled back with it.
func (s *Store) WithTx(ctx context.Context, fn func(*Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
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

	if err := fn(&Tx{q: tx}); err != nil {
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
