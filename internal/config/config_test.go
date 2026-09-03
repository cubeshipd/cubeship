package config

import "testing"

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
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) != 64 {
		t.Fatalf("expected a 64-hex-char generated token, got %d chars: %q", len(cfg.Token), cfg.Token)
	}
}
