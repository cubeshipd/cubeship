package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/user"
)

// readAdminKey returns the key ensureSuperAdmin persisted under dataDir.
func readAdminKey(t *testing.T, dataDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dataDir, adminKeyFileName))
	if err != nil {
		t.Fatalf("read admin key file: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func newUserService(t *testing.T) *user.Service {
	t.Helper()
	return user.NewService(dbtest.New(t))
}

func TestEnsureSuperAdminCreatesOnFirstBoot(t *testing.T) {
	users := newUserService(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	if err := ensureSuperAdmin(ctx, users, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	u, err := users.Repo().ByAPIKeyHash(ctx, authkey.Hash(readAdminKey(t, dataDir)))
	if err != nil {
		t.Fatalf("the persisted key does not authenticate: %v", err)
	}
	if !u.IsSuperAdmin {
		t.Fatal("expected the bootstrapped user to be a super-admin")
	}

	info, err := os.Stat(filepath.Join(dataDir, adminKeyFileName))
	if err != nil {
		t.Fatalf("stat admin key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected the admin key file to be 0600, got %04o", perm)
	}
}

// The super-admin's API key must not be the daemon's system token: that
// token is only the registry webhook's shared secret. Conflating them
// would mean anyone holding it could also create organizations and read
// every app's environment.
func TestEnsureSuperAdminKeyIsNotTheDaemonToken(t *testing.T) {
	users := newUserService(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	// The value config.Load would hand the registry and the webhook.
	const daemonToken = "the-daemon-system-token"
	if err := os.WriteFile(filepath.Join(dataDir, "token"), []byte(daemonToken+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	if err := ensureSuperAdmin(ctx, users, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	if _, err := users.Repo().ByAPIKeyHash(ctx, authkey.Hash(daemonToken)); err == nil {
		t.Fatal("the daemon's registry/webhook token must not authenticate as the super-admin")
	}
	if key := readAdminKey(t, dataDir); key == daemonToken || key == "" {
		t.Fatalf("expected a separate generated admin key, got %q", key)
	}
}

// The daemon runs this on every start, so a second call must neither
// create a second user nor replace the operator's saved key.
func TestEnsureSuperAdminIsIdempotent(t *testing.T) {
	users := newUserService(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	if err := ensureSuperAdmin(ctx, users, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin (first call): %v", err)
	}
	firstKey := readAdminKey(t, dataDir)
	if err := ensureSuperAdmin(ctx, users, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin (second call): %v", err)
	}

	n, err := users.Repo().Count(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 user after two calls, got %d", n)
	}
	if readAdminKey(t, dataDir) != firstKey {
		t.Fatal("expected the persisted admin key to be reused, not regenerated")
	}
}

// An existing key file is reused, so re-seeding a wiped database doesn't
// silently invalidate the operator's saved credentials.
func TestEnsureSuperAdminReusesPersistedKey(t *testing.T) {
	dataDir := t.TempDir()
	const existing = "0123456789abcdef"
	if err := os.WriteFile(filepath.Join(dataDir, adminKeyFileName), []byte(existing+"\n"), 0o600); err != nil {
		t.Fatalf("write admin key file: %v", err)
	}

	users := newUserService(t)
	if err := ensureSuperAdmin(context.Background(), users, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}
	if _, err := users.Repo().ByAPIKeyHash(context.Background(), authkey.Hash(existing)); err != nil {
		t.Fatalf("expected the persisted key to authenticate: %v", err)
	}
}
