package store

import (
	"context"
	"testing"
)

func TestCreateAPIKeyAndLookupByHash(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)

	if _, err := s.CreateAPIKey(ctx, user.ID, "hash-of-secret"); err != nil {
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

func TestRevokeAPIKeysForUser(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "old-hash")

	if err := s.RevokeAPIKeysForUser(ctx, user.ID); err != nil {
		t.Fatalf("RevokeAPIKeysForUser: %v", err)
	}
	if _, err := s.GetUserByAPIKeyHash(ctx, "old-hash"); err == nil {
		t.Fatal("expected the revoked key to no longer resolve")
	}
}

func TestTouchAPIKeyLastUsed(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	user, _ := s.CreateUser(ctx, "lucas", true)
	s.CreateAPIKey(ctx, user.ID, "a-hash")

	if err := s.TouchAPIKeyLastUsed(ctx, "a-hash"); err != nil {
		t.Fatalf("TouchAPIKeyLastUsed: %v", err)
	}

	var lastUsed *string
	s.db.QueryRow(`SELECT last_used_at FROM api_keys WHERE key_hash = ?`, "a-hash").Scan(&lastUsed)
	if lastUsed == nil {
		t.Fatal("expected last_used_at to be set")
	}
}
