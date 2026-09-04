package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreateAPIKeyAndLookupByHash(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)

	if _, err := s.CreateAPIKey(ctx, user.ID, "hash-of-secret", "default"); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	got, err := s.GetUserByAPIKeyHash(ctx, "hash-of-secret")
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("expected user %d, got %d", user.ID, got.ID)
	}
}

func TestGetUserByAPIKeyHashUnknown(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if _, err := s.GetUserByAPIKeyHash(context.Background(), "no-such-hash"); err == nil {
		t.Fatal("expected an error for an unknown key hash")
	}
}

func TestGetAPIKeyByHash(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "a-hash", "mcp")

	got, err := s.GetAPIKeyByHash(ctx, "a-hash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got.UserID != user.ID || got.Name != "mcp" {
		t.Fatalf("unexpected key: %+v", got)
	}
}

func TestListAPIKeysForUser(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	other, _ := s.CreateUser(ctx, "other", false)
	s.CreateAPIKey(ctx, user.ID, "hash-1", "default")
	s.CreateAPIKey(ctx, user.ID, "hash-2", "mcp")
	s.CreateAPIKey(ctx, other.ID, "hash-3", "default")

	keys, err := s.ListAPIKeysForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListAPIKeysForUser: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys for user, got %d", len(keys))
	}
	names := map[string]bool{}
	for _, k := range keys {
		names[k.Name] = true
	}
	if !names["default"] || !names["mcp"] {
		t.Fatalf("expected both key names present, got %v", keys)
	}
}

func TestRevokeAPIKeyByHashLeavesOtherKeysAlone(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "old-hash", "default")
	s.CreateAPIKey(ctx, user.ID, "other-hash", "mcp")

	if err := s.RevokeAPIKeyByHash(ctx, "old-hash"); err != nil {
		t.Fatalf("RevokeAPIKeyByHash: %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "old-hash"); err == nil {
		t.Fatal("expected the revoked key to no longer resolve")
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "other-hash"); err != nil {
		t.Fatalf("expected the other key to still resolve: %v", err)
	}
}

func TestRevokeAPIKeyByID(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	key, _ := s.CreateAPIKey(ctx, user.ID, "hash-1", "default")

	if err := s.RevokeAPIKeyByID(ctx, key.ID, user.ID); err != nil {
		t.Fatalf("RevokeAPIKeyByID: %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "hash-1"); err == nil {
		t.Fatal("expected the revoked key to no longer resolve")
	}
}

// A user must never be able to revoke another user's key by guessing its
// id — RevokeAPIKeyByID is scoped to the caller's own userID.
func TestRevokeAPIKeyByIDRefusesAnotherUsersKey(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	owner, _ := s.CreateUser(ctx, "owner", false)
	attacker, _ := s.CreateUser(ctx, "attacker", false)
	key, _ := s.CreateAPIKey(ctx, owner.ID, "hash-1", "default")

	if err := s.RevokeAPIKeyByID(ctx, key.ID, attacker.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "hash-1"); err != nil {
		t.Fatalf("expected the owner's key to survive: %v", err)
	}
}

func TestTouchAPIKeyLastUsed(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "a-hash", "default")

	if err := s.TouchAPIKeyLastUsed(ctx, "a-hash"); err != nil {
		t.Fatalf("TouchAPIKeyLastUsed: %v", err)
	}

	var lastUsed *string
	s.db.QueryRow(`SELECT last_used_at FROM api_keys WHERE key_hash = ?`, "a-hash").Scan(&lastUsed)
	if lastUsed == nil {
		t.Fatal("expected last_used_at to be set")
	}
}
