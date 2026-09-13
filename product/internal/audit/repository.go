package audit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

const columns = `id, at, COALESCE(user_id, 0), username, via, COALESCE(key_id, 0), key_name, action, target, outcome, status, detail, ip`

func (r *Repository) Insert(ctx context.Context, e Event) error {
	var userID, keyID any
	if e.UserID != 0 {
		userID = e.UserID
	}
	if e.KeyID != 0 {
		keyID = e.KeyID
	}
	_, err := r.q.ExecContext(ctx, `
		INSERT INTO audit_events (user_id, username, via, key_id, key_name, action, target, outcome, status, detail, ip)
		VALUES ((SELECT id FROM users WHERE id = $1), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		userID, e.Username, string(e.Via), keyID, e.KeyName, e.Action, e.Target, string(e.Outcome), e.Status, e.Detail, e.IP)
	if err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}

func (r *Repository) List(ctx context.Context, f Filter) ([]*Event, error) {
	var where []string
	var args []any
	add := func(clause string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.Username != "" {
		add("username = ?", f.Username)
	}
	if f.Via != "" {
		add("via = ?", string(f.Via))
	}
	if f.Outcome != "" {
		add("outcome = ?", string(f.Outcome))
	}
	if f.Target != "" {
		add("strpos(target, ?) > 0", f.Target)
	}
	if f.Before > 0 {
		add("id < ?", f.Before)
	}
	query := `SELECT ` + columns + ` FROM audit_events`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, " AND ")
	}
	args = append(args, f.Limit)
	query += ` ORDER BY id DESC LIMIT $` + strconv.Itoa(len(args))

	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	out := []*Event{}
	for rows.Next() {
		var e Event
		var via, outcome string
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.Username, &via, &e.KeyID, &e.KeyName,
			&e.Action, &e.Target, &outcome, &e.Status, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		e.Via, e.Outcome = Via(via), Outcome(outcome)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// Purge deletes events older than before, and reports how many.
func (r *Repository) Purge(ctx context.Context, before time.Time) (int64, error) {
	res, err := r.q.ExecContext(ctx, `DELETE FROM audit_events WHERE at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("purge audit events: %w", err)
	}
	return res.RowsAffected()
}
