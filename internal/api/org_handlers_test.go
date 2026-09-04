package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
	"cubeship/internal/storetest"
)

func TestCreateOrgRequiresSuperAdmin(t *testing.T) {
	s := storetest.New(t)
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
	s := storetest.New(t)
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

// The slug becomes a path component of every image reference the org
// pushes (registry.<domain>/<slug>/<app>), so a slug Docker would reject
// has to be rejected here instead of at `docker push` time.
func TestCreateOrgRejectsInvalidSlugs(t *testing.T) {
	s := storetest.New(t)
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	adminKey := testAPIKeyFor(t, s, true)

	for _, slug := range []string{"Acme", "acme corp", "acme/evil", "-acme", "acme-", "acme_corp", "ACME", "acme:latest", "."} {
		body, _ := json.Marshal(map[string]string{"slug": slug, "name": "Acme Inc"})
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/orgs", body, adminKey))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("slug %q: expected 400, got %d: %s", slug, rec.Code, rec.Body.String())
		}
		if _, err := s.GetOrganizationBySlug(context.Background(), slug); err == nil {
			t.Fatalf("slug %q: expected no organization to be created", slug)
		}
	}

	for _, slug := range []string{"acme", "acme-corp", "a", "acme2", "1acme"} {
		body, _ := json.Marshal(map[string]string{"slug": slug, "name": "Acme Inc"})
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/orgs", body, adminKey))
		if rec.Code != http.StatusCreated {
			t.Fatalf("slug %q: expected 201, got %d: %s", slug, rec.Code, rec.Body.String())
		}
	}
}

// A duplicate slug used to surface the raw SQLite constraint error as a
// 500.
func TestCreateOrgDuplicateSlugConflicts(t *testing.T) {
	s := storetest.New(t)
	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	adminKey := testAPIKeyFor(t, s, true)

	body, _ := json.Marshal(map[string]string{"slug": "acme", "name": "Acme Inc"})
	first := httptest.NewRecorder()
	srv.Router().ServeHTTP(first, authedRequest(http.MethodPost, "/orgs", body, adminKey))
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", first.Code)
	}

	second := httptest.NewRecorder()
	srv.Router().ServeHTTP(second, authedRequest(http.MethodPost, "/orgs", body, adminKey))
	if second.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", second.Code, second.Body.String())
	}
}

func TestListOrgsSuperAdminSeesAll(t *testing.T) {
	s := storetest.New(t)
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
	s := storetest.New(t)
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
