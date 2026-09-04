package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRequiresDomain(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when CUBESHIP_DOMAIN is unset")
	}
}

func TestLoadDerivesHostsFromDomain(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "fixed-token")
	t.Setenv("CUBESHIP_DATA_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RegistryHost != "registry.example.com" {
		t.Fatalf("expected registry.example.com, got %q", cfg.RegistryHost)
	}
	if cfg.APIHost != "api.example.com" {
		t.Fatalf("expected api.example.com, got %q", cfg.APIHost)
	}
	if cfg.Token != "fixed-token" {
		t.Fatalf("expected the provided token to be used, got %q", cfg.Token)
	}
	if cfg.DataDir != "/var/lib/cubeship" {
		t.Fatalf("expected the default data dir, got %q", cfg.DataDir)
	}
	if cfg.AcmeEmail != "admin@example.com" {
		t.Fatalf("expected the ACME email to be read, got %q", cfg.AcmeEmail)
	}
}

func TestLoadRequiresAcmeEmail(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when CUBESHIP_ACME_EMAIL is unset")
	}
}

func TestLoadGeneratesTokenWhenUnset(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")
	t.Setenv("CUBESHIP_DATA_DIR", dataDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) != 64 {
		t.Fatalf("expected a 64-hex-char generated token, got %d chars: %q", len(cfg.Token), cfg.Token)
	}
}

func TestLoadPersistsGeneratedTokenAcrossRestarts(t *testing.T) {
	// A token regenerated on every restart silently invalidates every
	// saved CLI credential and the registry login.
	dataDir := t.TempDir()
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")
	t.Setenv("CUBESHIP_DATA_DIR", dataDir)

	first, err := Load()
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	second, err := Load()
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if first.Token != second.Token {
		t.Fatalf("expected the token to survive a restart, got %q then %q", first.Token, second.Token)
	}

	tokenPath := filepath.Join(dataDir, "token")
	if first.TokenFile != tokenPath {
		t.Fatalf("expected TokenFile to point at %q, got %q", tokenPath, first.TokenFile)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("expected the token file to exist: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 on the token file, got %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(tokenPath)
	if strings.TrimSpace(string(data)) != first.Token {
		t.Fatalf("token file content %q does not match the loaded token %q", data, first.Token)
	}
}

func TestLoadCreatesDataDirForTheTokenFile(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "nested", "cubeship")
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")
	t.Setenv("CUBESHIP_DATA_DIR", dataDir)

	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "token")); err != nil {
		t.Fatalf("expected the data dir to be created for the token file: %v", err)
	}
}

func TestLoadReplacesEmptyTokenFile(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "token"), []byte("\n"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")
	t.Setenv("CUBESHIP_DATA_DIR", dataDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) != 64 {
		t.Fatalf("expected a fresh token to replace a truncated file, got %q", cfg.Token)
	}
}

func TestLoadPrefersEnvTokenOverFile(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "token"), []byte("persisted-token\n"), 0o600); err != nil {
		t.Fatalf("seed token file: %v", err)
	}
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "env-token")
	t.Setenv("CUBESHIP_DATA_DIR", dataDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Token != "env-token" {
		t.Fatalf("expected CUBESHIP_TOKEN to win, got %q", cfg.Token)
	}
	if cfg.TokenFile != "" {
		t.Fatalf("expected no token file when the token comes from the environment, got %q", cfg.TokenFile)
	}
}

func TestTokenFingerprintHidesTheToken(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	fp := TokenFingerprint(token)

	if fp == "" || len(fp) != 8 {
		t.Fatalf("expected an 8-hex-char fingerprint, got %q", fp)
	}
	if strings.Contains(token, fp) {
		t.Fatalf("fingerprint %q leaks part of the token", fp)
	}
	if TokenFingerprint("another-token") == fp {
		t.Fatal("expected different tokens to fingerprint differently")
	}
}
