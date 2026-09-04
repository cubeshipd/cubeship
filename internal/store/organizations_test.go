package store

import (
	"context"
	"testing"
)

func TestCreateAndGetOrganization(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	ctx := context.Background()

	created, err := s.CreateOrganization(ctx, "acme", "Acme Inc")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetOrganizationBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("GetOrganizationBySlug: %v", err)
	}
	if got.Name != "Acme Inc" {
		t.Fatalf("expected name Acme Inc, got %q", got.Name)
	}
}

func TestGetOrganizationBySlugNotFound(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if _, err := s.GetOrganizationBySlug(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error for an unknown slug")
	}
}

func TestListOrganizations(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")

	orgs, err := s.ListOrganizations(ctx)
	if err != nil {
		t.Fatalf("ListOrganizations: %v", err)
	}
	if len(orgs) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(orgs))
	}
}
