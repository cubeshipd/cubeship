package node

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"cubeship/internal/platform/database"
)

type Repository struct {
	q database.Queryer
}

func NewRepository(q database.Queryer) *Repository { return &Repository{q: q} }

// columns is read in order by scan. Change one, change both.
//
// `token_hash` is not in it, and that is deliberate: nothing above this
// module ever needs the hash, and a column that is never selected is a
// column that cannot be rendered by accident. Authenticate looks it up
// by value instead.
const columns = `id, slug, description, control_plane, address, version,
	cores, memory_total_bytes, disk_total_bytes,
	cpu_percent, memory_bytes, disk_bytes, containers, mesh_node_id,
	last_seen_at, created_at`

type scanner interface{ Scan(dest ...any) error }

func scan(row scanner) (*Node, error) {
	var n Node
	var address, version sql.NullString
	var cpu sql.NullFloat64
	var memory, disk sql.NullInt64
	var lastSeen sql.NullTime
	if err := row.Scan(&n.ID, &n.Slug, &n.Description, &n.ControlPlane, &address, &version,
		&n.Cores, &n.MemoryTotalBytes, &n.DiskTotalBytes,
		&cpu, &memory, &disk, &n.Containers, &n.MeshNodeID, &lastSeen, &n.CreatedAt); err != nil {
		return nil, err
	}
	n.Address, n.Version = address.String, version.String
	if cpu.Valid {
		n.CPUPercent = &cpu.Float64
	}
	if memory.Valid {
		n.MemoryBytes = &memory.Int64
	}
	if disk.Valid {
		n.DiskBytes = &disk.Int64
	}
	if lastSeen.Valid {
		n.LastSeenAt = &lastSeen.Time
	}
	return &n, nil
}

// Create adds a worker and stores the hash of the credential it will
// authenticate with. The credential itself is the caller's to show
// once; nothing here can hand it back.
func (r *Repository) Create(ctx context.Context, slug, description, tokenHash string) (*Node, error) {
	row := r.q.QueryRowContext(ctx,
		`INSERT INTO nodes (slug, description, token_hash) VALUES ($1, $2, $3) RETURNING `+columns,
		slug, description, tokenHash)
	n, err := scan(row)
	if err != nil {
		if database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("create node: %w", err)
	}
	return n, nil
}

// List is the whole cluster, the control plane first and the rest by
// name. The control plane leads because it is the machine somebody is
// reading this on.
func (r *Repository) List(ctx context.Context) ([]*Node, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+columns+` FROM nodes ORDER BY control_plane DESC, slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Node
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repository) BySlug(ctx context.Context, slug string) (*Node, error) {
	n, err := scan(r.q.QueryRowContext(ctx, `SELECT `+columns+` FROM nodes WHERE slug = $1`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read node: %w", err)
	}
	return n, nil
}

// ByTokenHash is how an agent's credential becomes a node.
//
// A lookup by hash rather than a comparison against every row: the
// credential is 32 bytes of randomness, so there is nothing to guess by
// timing — the same reasoning that makes authkey.Hash right for an API
// key and wrong for a password.
func (r *Repository) ByTokenHash(ctx context.Context, hash string) (*Node, error) {
	n, err := scan(r.q.QueryRowContext(ctx,
		`SELECT `+columns+` FROM nodes WHERE token_hash = $1`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnknownToken
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate node: %w", err)
	}
	return n, nil
}

// Record writes what an agent last said about its machine, and stamps
// the pass. Everything here is the machine's own answer, so the newest
// one wins outright.
func (r *Repository) Record(ctx context.Context, id int64, rep Report) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE nodes SET address = $2, version = $3,
		        cores = $4, memory_total_bytes = $5, disk_total_bytes = $6,
		        cpu_percent = $7, memory_bytes = $8, disk_bytes = $9,
		        containers = $10, mesh_node_id = $11, last_seen_at = now()
		 WHERE id = $1`,
		id, rep.Address, rep.Version,
		rep.Cores, rep.MemoryTotalBytes, rep.DiskTotalBytes,
		rep.CPUPercent, rep.MemoryBytes, rep.DiskBytes, rep.Containers, rep.MeshNodeID)
	if err != nil {
		return fmt.Errorf("record what %d reported: %w", id, err)
	}
	return nil
}

// RecordMesh writes what the control plane's own Engine says about
// itself. Its row has no agent to report for it — it is the machine the
// daemon is running on — so the one thing that has to be kept fresh
// there is kept fresh here.
func (r *Repository) RecordMesh(ctx context.Context, id int64, address, meshNodeID string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE nodes SET address = $2, mesh_node_id = $3 WHERE id = $1`, id, address, meshNodeID)
	if err != nil {
		return fmt.Errorf("record the control plane's own address: %w", err)
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	_, err := r.q.ExecContext(ctx, `DELETE FROM nodes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	return nil
}
