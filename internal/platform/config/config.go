package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cubeship/internal/platform/authkey"
)

type Config struct {
	// Token is the instance-wide system credential: the shared secret on
	// the registry's push-notification webhook. It is NOT a user's API
	// key and NOT a registry login credential — registry push/pull now
	// goes through per-user tokens (see internal/regauth), and the
	// super-admin's own API key is generated separately (see
	// cmd/cubeshipd's adminKeyFileName).
	Token   string
	DataDir string

	// TokenFile is where a generated token is persisted. Empty when the
	// token came from CUBESHIP_TOKEN.
	TokenFile string

	// DatabaseURL is an externally provided Postgres connection string
	// (CUBESHIP_DATABASE_URL). When empty, the daemon brings up and owns
	// a Postgres container of its own — see bootstrap.PostgresDSN.
	DatabaseURL string

	// InContainer says the daemon is itself a container on the
	// "cubeship" network, which is how it ships. It changes every
	// address the daemon uses to reach its own infrastructure, and every
	// address that infrastructure is told to reach it back on: container
	// names either way, rather than loopback and host.docker.internal.
	//
	// A daemon running on the host — which is what `make dev` does — is
	// still supported, and is the reason this is a flag rather than an
	// assumption.
	InContainer bool

	// ControlPlane is the instance this daemon is a worker of, as a
	// base URL. Empty on the control plane itself, which is every
	// installation until somebody adds a second machine.
	//
	// Set means **worker mode**, and worker mode is a different program:
	// no database, no dashboard, no registry, no builder, and no
	// listening socket at all. See internal/worker.
	ControlPlane string

	// NodeToken is what this machine authenticates as, minted by the
	// control plane when the machine was added to the cluster. It is
	// not an API key and not anybody's: a node is not an account.
	NodeToken string

	// WebImage is the image the dashboard's container runs from.
	//
	// It is told rather than derived. The daemon could take its own
	// image reference and substitute the name, but that is string
	// surgery on a registry path an operator is free to change — and
	// getting it wrong means an instance whose dashboard silently
	// never starts. The daemon's own image bakes in the matching
	// version; install.sh overrides it when it builds locally.
	WebImage string
}

// ManagedDatabase reports whether the daemon is responsible for running
// the Postgres it connects to, rather than being pointed at one that
// already exists.
func (c *Config) ManagedDatabase() bool {
	return c.DatabaseURL == ""
}

// Load reads the configuration the daemon needs before it can reach its
// own database.
//
// The domain and the ACME contact address are deliberately not here any
// more: Cubeship is installed with one command and reached by IP, and
// those are configured from the dashboard afterwards. They are still read
// from the environment, but only to seed an install upgrading from the
// release where they were required — see SeedSettings.

// DataDirEnv names the one setting that cannot have a default worked
// out later: where the instance's state is.
const DataDirEnv = "CUBESHIP_DATA_DIR"

// DataDir is where this instance keeps its state.
//
// Its own function because one mode of this binary reads it without
// loading the rest of the configuration: the throwaway container that
// replaces the daemon has a socket and a directory and no business
// opening a database.
func DataDir() string {
	if dir := os.Getenv(DataDirEnv); dir != "" {
		return dir
	}
	return "/var/lib/cubeship"
}
func Load() (*Config, error) {
	dataDir := DataDir()

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

	controlPlane := strings.TrimRight(strings.TrimSpace(os.Getenv("CUBESHIP_CONTROL_PLANE")), "/")
	nodeToken := strings.TrimSpace(os.Getenv("CUBESHIP_NODE_TOKEN"))
	// Half a worker is not a mode. A daemon told where its control
	// plane is and not what to authenticate as would come up, dial, be
	// refused forever, and look like a machine that joined — so it
	// refuses to start and says which half is missing.
	if controlPlane != "" && nodeToken == "" {
		return nil, errors.New("CUBESHIP_CONTROL_PLANE is set without CUBESHIP_NODE_TOKEN: a worker needs the credential the control plane minted when the server was added")
	}
	if nodeToken != "" && controlPlane == "" {
		return nil, errors.New("CUBESHIP_NODE_TOKEN is set without CUBESHIP_CONTROL_PLANE: a worker needs the address of the instance it belongs to")
	}

	return &Config{
		Token:       token,
		DataDir:     dataDir,
		TokenFile:   tokenFile,
		DatabaseURL: os.Getenv("CUBESHIP_DATABASE_URL"),
		// Set in the image, not by whoever runs it.
		InContainer:  os.Getenv("CUBESHIP_IN_CONTAINER") == "1",
		WebImage:     os.Getenv("CUBESHIP_WEB_IMAGE"),
		ControlPlane: controlPlane,
		NodeToken:    nodeToken,
	}, nil
}

// Worker reports whether this daemon belongs to another instance rather
// than being one.
func (c *Config) Worker() bool { return c.ControlPlane != "" }

// SeedSettings returns the values an older release kept in the
// environment, for a one-time write into the settings table. Empty
// entries are ignored, and nothing already configured is overwritten.
func SeedSettings() map[string]string {
	return map[string]string{
		"domain":     os.Getenv("CUBESHIP_DOMAIN"),
		"acme_email": os.Getenv("CUBESHIP_ACME_EMAIL"),
	}
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

	token, err := authkey.Generate()
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

// TokenFingerprint is a short, non-secret identifier for a token, safe
// to write to logs where the token itself is not.
func TokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:4])
}
