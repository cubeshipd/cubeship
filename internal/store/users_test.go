package store

import (
	"context"
	"testing"
)

func TestCreateAndGetUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.CreateUser(ctx, "lucas", true)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !created.IsSuperAdmin {
		t.Fatal("expected IsSuperAdmin true")
	}

	byName, err := s.GetUserByUsername(ctx, "lucas")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	byID, err := s.GetUserByID(ctx, byName.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if byID.Username != "lucas" {
		t.Fatalf("expected username lucas, got %q", byID.Username)
	}
}

func TestCountUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 users, got %d", n)
	}

	s.CreateUser(ctx, "lucas", true)
	s.CreateUser(ctx, "employee1", false)

	n, err = s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 users, got %d", n)
	}
}
