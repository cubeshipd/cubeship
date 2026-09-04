package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
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

func TestEnsureSuperAdminCreatesOnFirstBoot(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := ensureSuperAdmin(ctx, s, "bootstrap-token"); err != nil {
		t.Fatalf("ensureSuperAdmin: %v", err)
	}

	user, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash("bootstrap-token"))
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if !user.IsSuperAdmin {
		t.Fatal("expected the bootstrapped user to be a super-admin")
	}
}

func TestEnsureSuperAdminIsIdempotent(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := ensureSuperAdmin(ctx, s, "first-token"); err != nil {
		t.Fatalf("ensureSuperAdmin (first call): %v", err)
	}
	if err := ensureSuperAdmin(ctx, s, "second-token"); err != nil {
		t.Fatalf("ensureSuperAdmin (second call): %v", err)
	}

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 user after two calls, got %d", n)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, authkey.Hash("second-token")); err == nil {
		t.Fatal("expected the second call to be a no-op, not seed a second key")
	}
}
