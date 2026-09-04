package clicreds

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")
	want := Credentials{BaseURL: "https://api.example.com", Token: "secret-token"}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected an error for a missing credentials file")
	}
}

func TestRegistryHostFromBaseURL(t *testing.T) {
	got, err := RegistryHostFromBaseURL("https://api.example.com")
	if err != nil {
		t.Fatalf("RegistryHostFromBaseURL: %v", err)
	}
	if got != "registry.example.com" {
		t.Fatalf("expected registry.example.com, got %q", got)
	}
}

func TestRegistryHostFromBaseURLRejectsUnexpectedHost(t *testing.T) {
	_, err := RegistryHostFromBaseURL("https://cubeship.example.com")
	if err == nil {
		t.Fatal("expected an error for a base URL that isn't api.<domain>")
	}
}
