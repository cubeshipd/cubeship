package database_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/pressly/goose/v3"

	"cubeship/internal/platform/database/dbtest"
)

// An instance upgrading to deploy webhooks already has apps, and every
// one of them gets a token of its own — not one shared, not an empty
// one that a webhook could be called with.
func TestTheDeployTokenMigrationGivesEveryExistingAppOne(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	provider, err := goose.NewProvider(goose.DialectPostgres, db.DB, os.DirFS("migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 72); err != nil {
		t.Fatalf("migrate down to before deploy tokens: %v", err)
	}

	var project, env int64
	if err := db.QueryRowContext(ctx, `INSERT INTO projects (slug) VALUES ('web') RETURNING id`).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO environments (project_id, slug) VALUES ($1, 'production') RETURNING id`, project).Scan(&env); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"api", "worker"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO apps (project_id, environment_id, name) VALUES ($1, $2, $3)`, project, env, name); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT deploy_token FROM apps`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]bool{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			t.Fatal(err)
		}
		if !hex.MatchString(token) || seen[token] {
			t.Errorf("token %q is malformed or shared", token)
		}
		seen[token] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("%d tokens, want one for each of the 2 apps", len(seen))
	}
}
