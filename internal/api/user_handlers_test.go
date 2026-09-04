package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/authkey"
	"cubeship/internal/store"
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

func TestWhoAmIReturnsCallerUsername(t *testing.T) {
	srv, _, _ := newTestServer(t)
	memberKey := testAPIKeyFor(t, srv.store, false)

	req := authedRequest(http.MethodGet, "/users/me", nil, memberKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["is_super_admin"] != false {
		t.Fatalf("expected is_super_admin false, got %v", got["is_super_admin"])
	}
	if got["username"] == "" || got["username"] == nil {
		t.Fatalf("expected a non-empty username, got %v", got["username"])
	}
}

// Rotating a key must never touch any OTHER key the same user holds —
// that independence is the entire point of letting a user hold several
// named keys (one for the CLI, one for an MCP client, say).
func TestRotateAPIKeyLeavesOtherKeysAlone(t *testing.T) {
	srv, cliKey, _ := newTestServer(t)

	createBody, _ := json.Marshal(map[string]string{"name": "mcp"})
	createRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(createRec, authedRequest(http.MethodPost, "/users/me/api-keys", createBody, cliKey))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create mcp key: expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	json.Unmarshal(createRec.Body.Bytes(), &created)
	mcpKey := created["api_key"].(string)

	rotateRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rotateRec, authedRequest(http.MethodPost, "/users/me/api-key/rotate", nil, cliKey))
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("rotate: expected 200, got %d: %s", rotateRec.Code, rotateRec.Body.String())
	}

	mcpReq := authedRequest(http.MethodGet, "/orgs", nil, mcpKey)
	mcpRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(mcpRec, mcpReq)
	if mcpRec.Code != http.StatusOK {
		t.Fatalf("expected the untouched mcp key to still work, got %d", mcpRec.Code)
	}
}

func TestCreateAPIKeyRequiresName(t *testing.T) {
	srv, key, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": ""})
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodPost, "/users/me/api-keys", body, key))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListAPIKeysMarksCurrentKey(t *testing.T) {
	srv, key, _ := newTestServer(t)
	createBody, _ := json.Marshal(map[string]string{"name": "mcp"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/users/me/api-keys", createBody, key))

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodGet, "/users/me/api-keys", nil, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got []apiKeyResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	var sawCurrent, sawMCP bool
	for _, k := range got {
		if k.CurrentKey {
			sawCurrent = true
			if k.Name != store.DefaultAPIKeyName {
				t.Fatalf("expected the default key to be marked current, got %q", k.Name)
			}
		}
		if k.Name == "mcp" {
			sawMCP = true
			if k.CurrentKey {
				t.Fatal("the newly created mcp key must not be marked current")
			}
		}
	}
	if !sawCurrent || !sawMCP {
		t.Fatalf("expected both the current default key and the mcp key, got %+v", got)
	}
}

func TestRevokeAPIKeyRemovesOnlyThatKey(t *testing.T) {
	srv, key, _ := newTestServer(t)
	createBody, _ := json.Marshal(map[string]string{"name": "mcp"})
	createRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(createRec, authedRequest(http.MethodPost, "/users/me/api-keys", createBody, key))
	var created map[string]any
	json.Unmarshal(createRec.Body.Bytes(), &created)
	mcpKeyID := int64(created["id"].(float64))
	mcpKey := created["api_key"].(string)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, fmt.Sprintf("/users/me/api-keys/%d", mcpKeyID), nil, key))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	revokedReq := authedRequest(http.MethodGet, "/orgs", nil, mcpKey)
	revokedRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(revokedRec, revokedReq)
	if revokedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected the revoked key to stop working, got %d", revokedRec.Code)
	}

	stillReq := authedRequest(http.MethodGet, "/orgs", nil, key)
	stillRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(stillRec, stillReq)
	if stillRec.Code != http.StatusOK {
		t.Fatalf("expected the original key to still work, got %d", stillRec.Code)
	}
}

func TestRevokeAPIKeyRefusesLastRemainingKey(t *testing.T) {
	srv, key, _ := newTestServer(t)

	keysRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(keysRec, authedRequest(http.MethodGet, "/users/me/api-keys", nil, key))
	var keys []apiKeyResponse
	json.Unmarshal(keysRec.Body.Bytes(), &keys)
	if len(keys) != 1 {
		t.Fatalf("expected exactly 1 key, got %d", len(keys))
	}

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, fmt.Sprintf("/users/me/api-keys/%d", keys[0].ID), nil, key))
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// One user must never be able to revoke another user's key by guessing
// its id.
func TestRevokeAPIKeyRefusesAnotherUsersKey(t *testing.T) {
	srv, key, _ := newTestServer(t)
	otherKey := testAPIKeyFor(t, srv.store, false)

	// Give the other user a second key too, so "their only remaining
	// key" doesn't confound this with TestRevokeAPIKeyRefusesLastRemainingKey.
	otherUser, err := srv.store.GetUserByAPIKeyHash(context.Background(), authkey.Hash(otherKey))
	if err != nil {
		t.Fatalf("GetUserByAPIKeyHash: %v", err)
	}
	if _, _, err := srv.createAdditionalAPIKey(context.Background(), otherUser, "second"); err != nil {
		t.Fatalf("createAdditionalAPIKey: %v", err)
	}
	keys, err := srv.store.ListAPIKeysForUser(context.Background(), otherUser.ID)
	if err != nil {
		t.Fatalf("ListAPIKeysForUser: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, authedRequest(http.MethodDelete, fmt.Sprintf("/users/me/api-keys/%d", keys[0].ID), nil, key))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	// Confirm the target user's key really does still work.
	stillReq := authedRequest(http.MethodGet, "/orgs", nil, otherKey)
	stillRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(stillRec, stillReq)
	if stillRec.Code != http.StatusOK {
		t.Fatalf("expected the other user's key to still work, got %d", stillRec.Code)
	}
}

func TestWhoAmIRejectsUnauthenticated(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/users/me", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
