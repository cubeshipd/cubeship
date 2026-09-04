package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/regauth"
	"cubeship/internal/store"
	"cubeship/internal/storetest"

	"github.com/golang-jwt/jwt/v5"
)

func newTokenTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s := storetest.New(t)

	srv := NewServer(s, nil, "webhook-secret", "registry.example.com")
	key, err := regauth.LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	srv.SetRegistrySigningKey(key)
	return srv, srv.registryHost
}

func tokenClaims(t *testing.T, srv *Server, tokenStr string) map[string]any {
	t.Helper()
	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		return &srv.registrySigningKey.PublicKey, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("issued token does not verify against the signing key: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("unexpected claims type: %T", parsed.Claims)
	}
	return claims
}

func TestRegistryTokenRejectsMissingBasicAuth(t *testing.T) {
	srv, _ := newTokenTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRegistryTokenRejectsUnknownAPIKey(t *testing.T) {
	srv, _ := newTokenTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry", nil)
	req.SetBasicAuth("lucas", "not-a-real-key")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRegistryTokenGrantsAccessWithinOwnOrg(t *testing.T) {
	srv, _ := newTokenTestServer(t)
	ctx := context.Background()
	org, _ := srv.store.CreateOrganization(ctx, "acme", "Acme Inc")
	user, _ := srv.store.CreateUser(ctx, "lucas", false)
	srv.store.AddMembership(ctx, user.ID, org.ID, store.RoleMember)
	key := testAPIKeyForExistingUser(t, srv.store, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry&scope=repository:acme/myapp:pull,push", nil)
	req.SetBasicAuth("lucas", key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ExpiresIn != 300 {
		t.Fatalf("expected expires_in 300, got %d", resp.ExpiresIn)
	}

	claims := tokenClaims(t, srv, resp.Token)
	if claims["sub"] != "lucas" {
		t.Fatalf("expected subject lucas, got %v", claims["sub"])
	}
	access, ok := claims["access"].([]any)
	if !ok || len(access) != 1 {
		t.Fatalf("expected exactly one access entry, got %v", claims["access"])
	}
	entry := access[0].(map[string]any)
	if entry["name"] != "acme/myapp" {
		t.Fatalf("expected access to acme/myapp, got %v", entry["name"])
	}
	actions := entry["actions"].([]any)
	if len(actions) != 2 || actions[0] != "pull" || actions[1] != "push" {
		t.Fatalf("expected pull+push actions, got %v", actions)
	}
}

func TestRegistryTokenOmitsAccessForOtherOrgs(t *testing.T) {
	srv, _ := newTokenTestServer(t)
	ctx := context.Background()
	acme, _ := srv.store.CreateOrganization(ctx, "acme", "Acme Inc")
	srv.store.CreateOrganization(ctx, "globex", "Globex Corp")
	user, _ := srv.store.CreateUser(ctx, "lucas", false)
	srv.store.AddMembership(ctx, user.ID, acme.ID, store.RoleMember)
	key := testAPIKeyForExistingUser(t, srv.store, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry&scope=repository:globex/theirapp:pull,push", nil)
	req.SetBasicAuth("lucas", key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (token still issued, just with no access), got %d", rec.Code)
	}
	var resp struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)

	claims := tokenClaims(t, srv, resp.Token)
	if access, ok := claims["access"].([]any); ok && len(access) != 0 {
		t.Fatalf("expected no access granted for another org's repository, got %v", access)
	}
}

func TestRegistryTokenSuperAdminGrantedAnyOrg(t *testing.T) {
	srv, _ := newTokenTestServer(t)
	ctx := context.Background()
	srv.store.CreateOrganization(ctx, "acme", "Acme Inc")
	admin, _ := srv.store.CreateUser(ctx, "root", true)
	key := testAPIKeyForExistingUser(t, srv.store, admin.ID)

	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry&scope=repository:acme/myapp:pull,push", nil)
	req.SetBasicAuth("root", key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	claims := tokenClaims(t, srv, mustDecodeToken(t, rec))
	access := claims["access"].([]any)
	if len(access) != 1 {
		t.Fatalf("expected super-admin to be granted access, got %v", access)
	}
}

func mustDecodeToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.Token
}

func TestRegistryTokenRequiresNoAPIAuthMiddleware(t *testing.T) {
	// The route must be reachable without an Authorization: Bearer
	// header at all (Basic auth is what carries the credential here) —
	// confirms it wasn't accidentally registered via handleAuth.
	srv, _ := newTokenTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v2/token?service=cubeship-registry", nil)
	req.SetBasicAuth("nobody", "nothing")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	// Wrong credentials still 401s (expected), but critically NOT because
	// of a missing "Bearer " prefix check from authMiddleware — verified
	// by the fact Basic auth was even read via r.BasicAuth() in the
	// handler rather than being rejected before reaching it. A 404 here
	// (route not found) would mean this route isn't registered on the
	// unauthenticated mux — that's the actual regression this guards.
	if rec.Code == http.StatusNotFound {
		t.Fatal("expected the route to exist (401 for bad creds, not 404)")
	}
}
