package user_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"cubeship/internal/server/servertest"
)

// The multi-key model exists so an agent's credential and a terminal's
// credential are independent. If rotating one killed the other, holding
// two would be pointless.
func TestRotatingOneKeyLeavesEveryOtherKeyWorking(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		APIKey string `json:"api_key"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/users/me/api-keys",
		map[string]string{"name": "mcp"}, f.AdminKey, &created), http.StatusCreated)

	var rotated struct {
		APIKey string `json:"api_key"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/users/me/api-key/rotate",
		nil, f.AdminKey, &rotated), http.StatusOK)

	// The rotated key is dead, its replacement works, and the untouched
	// "mcp" key is unaffected.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, f.AdminKey), http.StatusUnauthorized)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, rotated.APIKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, created.APIKey), http.StatusOK)
}

// Revoking your last key would lock you out with no way back — and for
// the super-admin, no way back at all.
func TestRevokingYourOnlyKeyIsRefused(t *testing.T) {
	f := servertest.New(t)

	var keys []struct {
		ID         int64 `json:"id"`
		CurrentKey bool  `json:"current_key"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/users/me/api-keys", nil, f.AdminKey, &keys), http.StatusOK)
	if len(keys) != 1 {
		t.Fatalf("expected exactly one key to start with, got %d", len(keys))
	}
	if !keys[0].CurrentKey {
		t.Error("the key the request authenticated with should be marked as current")
	}

	rec := f.Do(t, http.MethodDelete, "/users/me/api-keys/"+strconv.FormatInt(keys[0].ID, 10), nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)

	// Still usable.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, f.AdminKey), http.StatusOK)
}

// Revoking is scoped to your own keys. Guessing another user's key id
// must look exactly like a key that doesn't exist — and must certainly
// not be reported as "that's your last key", which would confirm it.
func TestRevokingAnotherUsersKeyIsNotFound(t *testing.T) {
	f := servertest.New(t)
	_, victimKey := servertest.CreateUser(t, f.DB, "victim", false)

	var victimKeys []struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/users/me/api-keys", nil, victimKey, &victimKeys), http.StatusOK)

	// The admin holds two keys, so a "last key" refusal cannot mask this.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/users/me/api-keys",
		map[string]string{"name": "second"}, f.AdminKey), http.StatusCreated)

	rec := f.Do(t, http.MethodDelete, "/users/me/api-keys/"+strconv.FormatInt(victimKeys[0].ID, 10), nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusNotFound)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, victimKey), http.StatusOK)
}

// A key value is shown once, at creation, and never again.
func TestListedKeysNeverCarryTheKeyItself(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodGet, "/users/me/api-keys", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)

	var raw []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range raw {
		for _, field := range []string{"api_key", "key_hash", "key"} {
			if _, present := k[field]; present {
				t.Errorf("listing keys exposed %q: %v", field, k)
			}
		}
	}
}
