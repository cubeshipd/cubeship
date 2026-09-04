package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
)

func TestHealthzIsUnauthenticated(t *testing.T) {
	s := NewServer(nil, nil, "webhook-secret", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func newAuthMiddlewareTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ctx := context.Background()
	user, err := st.CreateUser(ctx, "test-user", false)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("authkey.Generate: %v", err)
	}
	if _, err := st.CreateAPIKey(ctx, user.ID, authkey.Hash(key), "default"); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	return NewServer(st, nil, "webhook-secret", "registry.example.com"), key
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	s, _ := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsValidAPIKey(t *testing.T) {
	s, key := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsUnknownAPIKey(t *testing.T) {
	s, _ := newAuthMiddlewareTestServer(t)
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-key")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewarePutsUserInContext(t *testing.T) {
	s, key := newAuthMiddlewareTestServer(t)
	var gotUser *store.User
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = userFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	protected.ServeHTTP(httptest.NewRecorder(), req)

	if gotUser == nil || gotUser.Username != "test-user" {
		t.Fatalf("expected the authenticated user in context, got %+v", gotUser)
	}
}
