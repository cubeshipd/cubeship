package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

type Config struct {
	Domain       string
	Token        string
	DataDir      string
	RegistryHost string
	APIHost      string
	AcmeEmail    string
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

	token := os.Getenv("CUBESHIP_TOKEN")
	if token == "" {
		generated, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		token = generated
	}

	dataDir := os.Getenv("CUBESHIP_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/cubeship"
	}

	return &Config{
		Domain:       domain,
		Token:        token,
		DataDir:      dataDir,
		RegistryHost: "registry." + domain,
		APIHost:      "api." + domain,
		AcmeEmail:    acmeEmail,
	}, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
