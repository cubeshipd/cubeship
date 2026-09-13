package setup

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// TokenFileName is where the setup token lives inside the data
// directory, beside the daemon's other generated credentials.
const TokenFileName = "setup-token"

// Token is the secret that has to be presented to claim an instance,
// and the file it was written to.
//
// It exists because the alternative is the unclaimed-Portainer problem:
// the installer publishes a port, and until somebody creates the first
// account, whoever reaches that port first becomes the admin of the
// machine. On a VPS with a public address that is a race the operator
// can lose between running the installer and opening their browser.
//
// The file is the delivery mechanism. It sits in the data directory,
// which only root can read, so possession of it stands for access to
// the host — which is what claiming the instance ought to require. The
// installer prints it; anyone else reads it over ssh.
//
// A zero Token means no token is configured, and Claim asks for none.
// That is what a test builds, and it is what an instance already claimed
// has: the file is removed the moment setup succeeds, because a
// credential that can no longer do anything should not be left lying
// in a directory that gets backed up.
type Token struct {
	Value string
	Path  string
}

// Required reports whether claiming this instance needs a token.
func (t Token) Required() bool { return t.Value != "" }

// matches compares a presented token in constant time.
func (t Token) matches(presented string) bool {
	return subtle.ConstantTimeCompare([]byte(t.Value), []byte(presented)) == 1
}

// forget removes the file. Called once setup has succeeded, when the
// token can no longer claim anything.
func (t Token) forget() {
	if t.Path == "" {
		return
	}
	if err := os.Remove(t.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Not fatal: the instance is claimed, so the token is already
		// worthless. Worth saying, because a file the operator was told
		// to read is still sitting there.
		log.Printf("setup: could not remove the spent setup token at %s: %v", t.Path, err)
	}
}

// EnsureToken returns the token guarding an unclaimed instance, writing
// one on first start.
//
// An instance that already has an account needs none, and any file left
// from before it was claimed is removed here — that is the path a daemon
// takes when it restarts after a claim that could not delete it.
func EnsureToken(ctx context.Context, db *database.DB, dataDir string) (Token, error) {
	path := filepath.Join(dataDir, TokenFileName)

	n, err := user.NewRepository(db).Count(ctx)
	if err != nil {
		return Token{}, fmt.Errorf("count accounts: %w", err)
	}
	if n > 0 {
		Token{Path: path}.forget()
		return Token{}, nil
	}

	// A token already written is kept: regenerating it on every restart
	// would invalidate the one the installer printed.
	if data, err := os.ReadFile(path); err == nil {
		if value := strings.TrimSpace(string(data)); value != "" {
			return Token{Value: value, Path: path}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Token{}, fmt.Errorf("read %s: %w", path, err)
	}

	value, err := authkey.Generate()
	if err != nil {
		return Token{}, fmt.Errorf("generate a setup token: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return Token{}, fmt.Errorf("create data dir %s: %w", dataDir, err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return Token{}, fmt.Errorf("write %s: %w", path, err)
	}
	return Token{Value: value, Path: path}, nil
}
