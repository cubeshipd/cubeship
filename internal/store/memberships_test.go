package store

import (
	"context"
	"testing"
)

func TestAddAndGetMembership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "lucas", false)

	if err := s.AddMembership(ctx, user.ID, org.ID, RoleAdmin); err != nil {
		t.Fatalf("AddMembership: %v", err)
	}

	role, err := s.GetMembership(ctx, user.ID, org.ID)
	if err != nil {
		t.Fatalf("GetMembership: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("expected admin, got %q", role)
	}
}

func TestGetMembershipNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "lucas", false)

	if _, err := s.GetMembership(ctx, user.ID, org.ID); err == nil {
		t.Fatal("expected an error for a user with no membership in the org")
	}
}

func TestListMembershipsForUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	acme, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	globex, _ := s.CreateOrganization(ctx, "globex", "Globex Corp")
	user, _ := s.CreateUser(ctx, "lucas", false)
	s.AddMembership(ctx, user.ID, acme.ID, RoleAdmin)
	s.AddMembership(ctx, user.ID, globex.ID, RoleMember)

	memberships, err := s.ListMembershipsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListMembershipsForUser: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(memberships))
	}
	bySlug := map[string]Role{}
	for _, m := range memberships {
		bySlug[m.OrgSlug] = m.Role
	}
	if bySlug["acme"] != RoleAdmin || bySlug["globex"] != RoleMember {
		t.Fatalf("unexpected memberships: %+v", memberships)
	}
}
