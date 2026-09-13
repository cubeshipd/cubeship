package discovery

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Postgres is the catalog, in the database cubeship.dev reads.
type Postgres struct {
	DB *sql.DB
}

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies the catalog's schema. This service owns these tables;
// the site only ever reads them.
func (p *Postgres) Migrate(ctx context.Context) error {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, p.DB, fsys)
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// lockKey is one arbitrary constant: two copies of this service must
// not index the same release at once.
const lockKey = 4_212_101

// Locked runs fn while holding the catalog's advisory lock, and reports
// false without running it when another copy holds it.
func (p *Postgres) Locked(ctx context.Context, fn func() error) (bool, error) {
	conn, err := p.DB.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var got bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockKey).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, lockKey)
	return true, fn()
}

func (p *Postgres) Blocked(ctx context.Context) (map[string]bool, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT lower(subject) FROM blocklist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = true
	}
	return out, rows.Err()
}

func (p *Postgres) Repositories(ctx context.Context) ([]Known, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT id, node_id FROM repositories ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Known
	for rows.Next() {
		var k Known
		if err := rows.Scan(&k.ID, &k.NodeID); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (p *Postgres) SaveRepository(ctx context.Context, r Repo, hidden string) error {
	topics := r.Topics
	if topics == nil {
		topics = []string{}
	}
	_, err := p.DB.ExecContext(ctx, `
		INSERT INTO repositories (id, node_id, owner, name, description, url, owner_avatar_url, stars, topics, hidden)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
		ON CONFLICT (id) DO UPDATE SET
			node_id = EXCLUDED.node_id, owner = EXCLUDED.owner, name = EXCLUDED.name,
			description = EXCLUDED.description, url = EXCLUDED.url,
			owner_avatar_url = EXCLUDED.owner_avatar_url, stars = EXCLUDED.stars,
			topics = EXCLUDED.topics, hidden = EXCLUDED.hidden, checked_at = now()`,
		r.ID, r.NodeID, r.Owner, r.Name, r.Description, r.URL, r.OwnerAvatar, r.Stars, topics, hidden)
	return err
}

func (p *Postgres) Hide(ctx context.Context, id int64, reason string) error {
	_, err := p.DB.ExecContext(ctx, `UPDATE repositories SET hidden = $2, checked_at = now() WHERE id = $1`, id, reason)
	return err
}

func (p *Postgres) Indexed(ctx context.Context, repositoryID int64, tag, commit string) (bool, error) {
	var done bool
	err := p.DB.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM releases WHERE repository_id = $1 AND tag = $2 AND commit_sha = $3)`,
		repositoryID, tag, commit).Scan(&done)
	return done, err
}

func (p *Postgres) SaveRelease(ctx context.Context, r Indexed) error {
	problems, err := json.Marshal(r.Problems)
	if err != nil {
		return err
	}
	var manifest []byte
	if r.Manifest != nil {
		if manifest, err = json.Marshal(r.Manifest); err != nil {
			return err
		}
	}
	status := "rejected"
	if r.Accepted {
		status = "accepted"
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO releases (repository_id, tag, commit_sha, name, url, published_at, status, problems, manifest, source, readme, icon_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''))
		ON CONFLICT (repository_id, tag, commit_sha) DO NOTHING`,
		r.RepositoryID, r.Tag, r.Commit, r.Name, r.URL, r.PublishedAt, status, problems, nullJSON(manifest),
		r.Source, r.Readme, r.IconKey); err != nil {
		return err
	}
	// The newest accepted release is what the catalog shows: a rejected
	// one published after it leaves the listing where it was.
	if _, err := tx.ExecContext(ctx, `
		UPDATE repositories SET latest_release_id = (
			SELECT id FROM releases WHERE repository_id = $1 AND status = 'accepted'
			ORDER BY published_at DESC, id DESC LIMIT 1
		) WHERE id = $1`, r.RepositoryID); err != nil {
		return err
	}
	return tx.Commit()
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}
