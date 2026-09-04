package main

import (
	"os"
	"path/filepath"
	"testing"
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
