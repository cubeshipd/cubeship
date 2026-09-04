package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func TestCreateOrgRequiresSuperAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	nonAdminKey := testAPIKeyFor(t, s, false)

	body, _ := json.Marshal(map[string]string{"slug": "acme", "name": "Acme Inc"})
	req := authedRequest(http.MethodPost, "/orgs", body, nonAdminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgAsSuperAdmin(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	adminKey := testAPIKeyFor(t, s, true)

	body, _ := json.Marshal(map[string]string{"slug": "acme", "name": "Acme Inc"})
	req := authedRequest(http.MethodPost, "/orgs", body, adminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["slug"] != "acme" || got["name"] != "Acme Inc" {
		t.Fatalf("unexpected response: %v", got)
	}
}

func TestListOrgsSuperAdminSeesAll(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")
	adminKey := testAPIKeyFor(t, s, true)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := authedRequest(http.MethodGet, "/orgs", nil, adminKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	var got []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 orgs, got %d: %v", len(got), got)
	}
}

func TestListOrgsMemberSeesOnlyTheirOwn(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	acme, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateOrganization(ctx, "globex", "Globex Corp")
	user, _ := s.CreateUser(ctx, "employee1", false)
	s.AddMembership(ctx, user.ID, acme.ID, store.RoleMember)
	rawKey := testAPIKeyForExistingUser(t, s, user.ID)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	req := authedRequest(http.MethodGet, "/orgs", nil, rawKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	var got []map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 1 || got[0]["slug"] != "acme" {
		t.Fatalf("expected only acme, got %v", got)
	}
}
