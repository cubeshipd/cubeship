package store

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

// ErrNotFound is what the store's Get* methods wrap when no row matches,
// so callers can tell "no such row" from a real database failure without
// importing database/sql themselves.
var ErrNotFound = sql.ErrNoRows

const schema = `
CREATE TABLE IF NOT EXISTS organizations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	slug TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	org_id INTEGER NOT NULL REFERENCES organizations(id),
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	env TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deployments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id),
	image_ref TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	is_super_admin INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memberships (
	user_id INTEGER NOT NULL REFERENCES users(id),
	org_id INTEGER NOT NULL REFERENCES organizations(id),
	role TEXT NOT NULL,
	PRIMARY KEY (user_id, org_id)
);

CREATE TABLE IF NOT EXISTS api_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	key_hash TEXT NOT NULL UNIQUE,
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	last_used_at DATETIME
);
`

// DefaultOrgSlug is the organization pre-existing apps are adopted into
// when a database created before organizations existed is upgraded. See
// migrate.
const DefaultOrgSlug = "default"

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate brings an older database up to the current schema. The
// statements above are all CREATE TABLE IF NOT EXISTS, which is a no-op
// against a table that already exists — so a column added to the apps
// table never appears on an upgraded database unless it is added here
// explicitly. Without this, a daemon upgraded from an older release
// starts, then fails every apps query with "no such column: org_id"
// (or, from further back, "no such column: env").
func migrate(db *sql.DB) error {
	hasEnv, err := hasColumn(db, "apps", "env")
	if err != nil {
		return err
	}
	hasOrgID, err := hasColumn(db, "apps", "org_id")
	if err != nil {
		return err
	}
	if hasEnv && hasOrgID {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	if !hasEnv {
		if _, err := tx.Exec(`ALTER TABLE apps ADD COLUMN env TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("add apps.env: %w", err)
		}
	}

	if !hasOrgID {
		// DEFAULT 0 is what makes this possible at all: SQLite requires
		// a non-null default to add a NOT NULL column to a table with
		// rows. The rows it leaves behind (org_id = 0, no such
		// organization) are adopted below.
		if _, err := tx.Exec(`ALTER TABLE apps ADD COLUMN org_id INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add apps.org_id: %w", err)
		}

		var orphaned int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM apps WHERE org_id = 0`).Scan(&orphaned); err != nil {
			return fmt.Errorf("count unowned apps: %w", err)
		}
		if orphaned > 0 {
			// Existing apps must end up owned by a real organization, or
			// every authorization check against them fails and their
			// owner can no longer see them.
			orgID, err := ensureDefaultOrg(tx)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE apps SET org_id = ? WHERE org_id = 0`, orgID); err != nil {
				return fmt.Errorf("adopt unowned apps: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// ensureDefaultOrg returns the id of the DefaultOrgSlug organization,
// creating it if this is the first upgrade.
func ensureDefaultOrg(tx *sql.Tx) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM organizations WHERE slug = ?`, DefaultOrgSlug).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("look up default organization: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO organizations (slug, name) VALUES (?, ?)`, DefaultOrgSlug, "Default")
	if err != nil {
		return 0, fmt.Errorf("create default organization: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// hasColumn reports whether table has a column of the given name.
func hasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) Close() error {
	return s.db.Close()
}
