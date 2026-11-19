package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/config"
)

// The daemon generates a few secrets for itself on first start and reuses
// them forever after. Regenerating one would be quietly destructive: the
// Postgres password only takes effect when the data directory is
// initialized, so a new one on restart simply stops matching the
// database.
func TestLoadOrCreateSecretIsStableAndPrivate(t *testing.T) {
	dataDir := t.TempDir()

	first, path, err := loadOrCreateSecret(dataDir, pgPasswordFileName)
	if err != nil {
		t.Fatalf("loadOrCreateSecret: %v", err)
	}
	if first == "" {
		t.Fatal("generated an empty secret")
	}

	again, _, err := loadOrCreateSecret(dataDir, pgPasswordFileName)
	if err != nil {
		t.Fatalf("loadOrCreateSecret (second call): %v", err)
	}
	if again != first {
		t.Fatal("the secret was regenerated instead of reused")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret file is %04o, want 0600", perm)
	}
}

// A truncated write from an earlier crash leaves an empty file. Reading
// it back as a valid empty secret would be worse than replacing it.
func TestLoadOrCreateSecretReplacesAnEmptyFile(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, pgPasswordFileName), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	secret, _, err := loadOrCreateSecret(dataDir, pgPasswordFileName)
	if err != nil {
		t.Fatalf("loadOrCreateSecret: %v", err)
	}
	if secret == "" {
		t.Fatal("an empty file was accepted as the secret")
	}
}

// -reset-password reaches the database the daemon already uses, and
// must never mint a Postgres password of its own doing so: the daemon
// would start with it next time and stop matching its database.
func TestResetPasswordFindsTheDaemonsDatabaseAndCreatesNothing(t *testing.T) {
	external := &config.Config{DataDir: t.TempDir(), DatabaseURL: "postgres://elsewhere/db"}
	if dsn, err := databaseURL(external); err != nil || dsn != external.DatabaseURL {
		t.Fatalf("external database: got %q, %v", dsn, err)
	}

	cfg := &config.Config{DataDir: t.TempDir(), InContainer: true}
	path := filepath.Join(cfg.DataDir, pgPasswordFileName)
	if _, err := databaseURL(cfg); err == nil {
		t.Fatal("no password file was accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a password file was created: %v", err)
	}

	if err := os.WriteFile(path, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dsn, err := databaseURL(cfg)
	if err != nil {
		t.Fatalf("databaseURL: %v", err)
	}
	if want := bootstrap.PostgresDSN(cfg, "s3cret"); dsn != want {
		t.Fatalf("got %q, want %q", dsn, want)
	}
}
