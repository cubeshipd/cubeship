package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func TestAuthorizeOrgSuperAdminAlwaysPasses(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	admin, _ := s.CreateUser(ctx, "root", true)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, admin))

	if !srv.authorizeOrgRequest(req, org.ID, store.RoleAdmin) {
		t.Fatal("expected super-admin to be authorized regardless of membership")
	}
}

func TestAuthorizeOrgMemberPassesMemberButFailsAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "employee1", false)
	s.AddMembership(ctx, user.ID, org.ID, store.RoleMember)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, user))

	if !srv.authorizeOrgRequest(req, org.ID, store.RoleMember) {
		t.Fatal("expected a member to pass the member-level check")
	}
	if srv.authorizeOrgRequest(req, org.ID, store.RoleAdmin) {
		t.Fatal("expected a member to fail the admin-level check")
	}
}

func TestAuthorizeOrgNoMembershipFails(t *testing.T) {
	s, _ := store.Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := s.CreateUser(ctx, "outsider", false)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(
		context.WithValue(context.Background(), userContextKey, user))

	if srv.authorizeOrgRequest(req, org.ID, store.RoleMember) {
		t.Fatal("expected a user with no membership to be unauthorized")
	}
}
