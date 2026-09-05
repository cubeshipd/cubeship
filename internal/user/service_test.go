package user_test

import (
	"context"
	"cubeship/internal/user"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
	_, victimKey := servertest.CreateUser(t, f.DB, "victim", user.RoleMember)

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

// A laptop that walked off was, until now, valid forever: there was no
// way to end somebody else's session or revoke somebody else's key
// without opening the database by hand.
func TestRevokingAnotherAccountsCredentials(t *testing.T) {
	f := servertest.New(t)
	member, memberKey := f.AddMember(t, "member", user.RoleMember)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/users/me/password",
		map[string]string{"new_password": goodPassword}, memberKey), http.StatusOK)
	session := f.Login(t, "member", goodPassword)

	// Both credentials work before.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, memberKey), http.StatusOK)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session), http.StatusOK)

	// A member cannot do this to anyone, including themselves.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/admin/credentials", nil, memberKey),
		http.StatusForbidden)

	var revoked user.Revoked
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodDelete, "/users/member/credentials",
		nil, f.AdminKey, &revoked), http.StatusOK)
	if revoked.APIKeys != 1 || revoked.Sessions != 1 {
		t.Errorf("revoked %+v, want one of each", revoked)
	}

	// Neither works after, and the account is still there.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, memberKey), http.StatusUnauthorized)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session), http.StatusUnauthorized)

	if _, err := user.NewRepository(f.DB).ByID(context.Background(), member.ID); err != nil {
		t.Errorf("the account was deleted rather than having its credentials revoked: %v", err)
	}
}

// Deleting an account takes what it authenticates with, or the rows that
// let somebody in would outlive the account they belong to.
func TestDeletingAnAccountTakesItsCredentials(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/member", nil, f.AdminKey),
		http.StatusNoContent)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, memberKey),
		http.StatusUnauthorized)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/member", nil, f.AdminKey),
		http.StatusNotFound)
}

// The two accounts an instance must never lose: the one you are signed
// in as — a mistake nothing here could undo — and the last admin, since
// setup closed the moment the first account existed and nothing in the
// API can make an admin without one.
func TestTheInstanceKeepsAnAdminAndYourOwnAccount(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/admin", nil, f.AdminKey),
		http.StatusConflict)

	// A second admin, deleting the first: allowed, because one is left.
	_, otherKey := f.AddMember(t, "other", user.RoleAdmin)
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/admin", nil, otherKey),
		http.StatusNoContent)

	// And now that one is the last.
	servertest.RequireStatus(t, f.Do(t, http.MethodDelete, "/users/other", nil, otherKey),
		http.StatusConflict)
}

// The roster is who can reach this instance at all, so it is an
// admin's — and it never carries a key or a hash.
func TestListingAccounts(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users", nil, memberKey), http.StatusForbidden)

	rec := f.Do(t, http.MethodGet, "/users", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	for _, want := range []string{`"admin"`, `"member"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the roster does not list %s: %s", want, body)
		}
	}
	for _, leak := range []string{"key_hash", "api_key", "password"} {
		if strings.Contains(body, leak) {
			t.Errorf("the roster carries %q: %s", leak, body)
		}
	}
}
