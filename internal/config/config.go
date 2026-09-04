package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Domain string
	// Token is the instance-wide system credential: the shared secret on
	// the registry's push-notification webhook. It is NOT a user's API
	// key and NOT a registry login credential — registry push/pull now
	// goes through per-user tokens (see internal/regauth), and the
	// super-admin's own API key is generated separately (see
	// cmd/cubeshipd's loadOrCreateAdminKey).
	Token        string
	DataDir      string
	RegistryHost string
	APIHost      string
	AcmeEmail    string

	// TokenFile is where a generated token is persisted. Empty when the
	// token came from CUBESHIP_TOKEN.
	TokenFile string
}

func Load() (*Config, error) {
	domain := os.Getenv("CUBESHIP_DOMAIN")
	if domain == "" {
		return nil, fmt.Errorf("CUBESHIP_DOMAIN environment variable is required")
	}

	acmeEmail := os.Getenv("CUBESHIP_ACME_EMAIL")
	if acmeEmail == "" {
		return nil, fmt.Errorf("CUBESHIP_ACME_EMAIL environment variable is required")
	}

	dataDir := os.Getenv("CUBESHIP_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/cubeship"
	}

	var tokenFile string
	token := os.Getenv("CUBESHIP_TOKEN")
	if token == "" {
		tokenFile = filepath.Join(dataDir, "token")
		var err error
		token, err = loadOrCreateToken(dataDir, tokenFile)
		if err != nil {
			return nil, err
		}
	}

	return &Config{
		Domain:       domain,
		Token:        token,
		DataDir:      dataDir,
		RegistryHost: "registry." + domain,
		APIHost:      "api." + domain,
		AcmeEmail:    acmeEmail,
		TokenFile:    tokenFile,
	}, nil
}

// loadOrCreateToken reads the persisted system token, generating and
// storing one on first run. Persisting matters: a token regenerated on
// every restart silently invalidates every registry login and every
// notification the registry has queued.
func loadOrCreateToken(dataDir, path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
		// An empty file (a truncated write from a previous crash) is
		// treated as no token at all and replaced below.
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write token file %s: %w", path, err)
	}
	return token, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// TokenFingerprint is a short, non-secret identifier for a token, safe
// to write to logs where the token itself is not.
func TokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}
