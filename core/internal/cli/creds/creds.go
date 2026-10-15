package creds

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Credentials struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

func Save(path string, creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Load(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials (run 'cubeship login' first): %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	return creds, nil
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cubeship", "credentials.json"), nil
}

func RegistryHostFromBaseURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	if !strings.HasPrefix(u.Hostname(), "api.") {
		return "", fmt.Errorf("expected base URL host to start with 'api.', got %q", u.Hostname())
	}
	return "registry." + strings.TrimPrefix(u.Hostname(), "api."), nil
}
