package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/storetest"
)

func TestVersionFlag(t *testing.T) {
	out, err := exec.Command("go", "run", ".", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cubeshipd") {
		t.Fatalf("expected version output to contain cubeshipd, got: %s", out)
	}
}

// readAdminKey returns the key ensureSuperAdmin persisted under dataDir.
func readAdminKey(t *testing.T, dataDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dataDir, adminKeyFileName))
	if err != nil {
		t.Fatalf("read admin key file: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func TestEnsureSuperAdminCreatesOnFirstBoot(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	if err := ensureSuperAdmin(ctx, s, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	user, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash(readAdminKey(t, dataDir)))
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if !user.IsSuperAdmin {
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
// would mean anyone with the daemon token — including, previously,
// anyone who had to push before per-user registry tokens existed —
// holding a credential that also creates organizations and reads every
// app's environment.
func TestEnsureSuperAdminKeyIsNotTheDaemonToken(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	// The value config.Load would hand the registry and the webhook.
	const daemonToken = "the-daemon-system-token"
	if err := os.WriteFile(filepath.Join(dataDir, "token"), []byte(daemonToken+"\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	if err := ensureSuperAdmin(ctx, s, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	if _, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash(daemonToken)); err == nil {
		t.Fatal("the daemon's registry/webhook token must not authenticate as the super-admin")
	}
	if key := readAdminKey(t, dataDir); key == daemonToken || key == "" {
		t.Fatalf("expected a separate generated admin key, got %q", key)
	}
}

func TestEnsureSuperAdminIsIdempotent(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	dataDir := t.TempDir()

	if err := ensureSuperAdmin(ctx, s, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin (first call): %v", err)
	}
	firstKey := readAdminKey(t, dataDir)
	if err := ensureSuperAdmin(ctx, s, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin (second call): %v", err)
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
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

	s := storetest.New(t)
	ctx := context.Background()

	if err := ensureSuperAdmin(ctx, s, dataDir); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash(existing)); err != nil {
		t.Fatalf("expected the persisted key to authenticate: %v", err)
	}
}
