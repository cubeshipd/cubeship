package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateOrgUserRequiresOrgAdmin(t *testing.T) {
	srv, _, org := newTestServer(t)
	memberKey := testAPIKeyFor(t, srv.store, false)

	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "member"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, memberKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrgUserAsSuperAdmin(t *testing.T) {
	srv, key, org := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "member"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["username"] != "employee1" || got["role"] != "member" || got["api_key"] == "" {
		t.Fatalf("unexpected response: %v", got)
	}

	// The returned key actually works.
	req2 := authedRequest(http.MethodGet, "/orgs", nil, got["api_key"])
	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected the new user's key to authenticate, got %d", rec2.Code)
	}
}

func TestCreateOrgUserInvalidRole(t *testing.T) {
	srv, key, org := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"username": "employee1", "role": "owner"})
	req := authedRequest(http.MethodPost, "/orgs/"+org.Slug+"/users", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestRotateAPIKeyIssuesWorkingKeyAndRevokesOld(t *testing.T) {
	srv, oldKey, _ := newTestServer(t)

	req := authedRequest(http.MethodPost, "/users/me/api-key/rotate", nil, oldKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	json.Unmarshal(rec.Body.Bytes(), &got)
	newKey := got["api_key"]
	if newKey == "" || newKey == oldKey {
		t.Fatalf("expected a new, different key, got %q", newKey)
	}

	oldReq := authedRequest(http.MethodGet, "/orgs", nil, oldKey)
	oldRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(oldRec, oldReq)
	if oldRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the old key to be revoked, got %d", oldRec.Code)
	}

	newReq := authedRequest(http.MethodGet, "/orgs", nil, newKey)
	newRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(newRec, newReq)
	if newRec.Code != http.StatusOK {
		t.Fatalf("expected the new key to work, got %d", newRec.Code)
	}
}
