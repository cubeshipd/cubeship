package user_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/server/servertest"
)

// add creates an account through the API and returns its password.
func add(t *testing.T, f *servertest.Fixture, username, role string) string {
	t.Helper()
	var created struct {
		Password string `json:"password"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/users",
		map[string]string{"username": username, "role": role}, f.AdminKey, &created),
		http.StatusCreated)
	return created.Password
}

// issueKey makes an API key the only way one is ever made: by the
// account it belongs to, from its own session.
func issueKey(t *testing.T, f *servertest.Fixture, session *http.Cookie) string {
	t.Helper()
	rec := f.DoAs(t, http.MethodPost, "/users/me/api-keys",
		map[string]string{"name": "laptop"}, session)
	servertest.RequireStatus(t, rec, http.StatusCreated)
	var out struct {
		APIKey string `json:"api_key"`
	}
	decode(t, rec, &out)
	return out.APIKey
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
}

// **A blocked account is refused at every door, and there are three.**
//
// An API key, a session cookie and a password are three ways in, and a
// block that closed one of them would be a block that does nothing to
// somebody holding either of the others — which is the ordinary case,
// since a key on a laptop is exactly what an account being shut out is
// likely to be using.
//
// The password is the one worth spelling out: it is refused *after* the
// password is verified, so a wrong password on a blocked account is
// still "incorrect username or password" and does not say the account
// exists.
func TestABlockedAccountIsRefusedAtEveryDoor(t *testing.T) {
	f := servertest.New(t)
	password := add(t, f, "ana", "member")

	// A key of her own, made the only way keys are made: by her.
	session := f.Login(t, "ana", password)
	key := issueKey(t, f, session)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, key), http.StatusOK)

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/ana", map[string]any{"blocked": true}, f.AdminKey), http.StatusOK)

	// The key she was using, the session she was using, and signing in
	// again — all three.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, key),
		http.StatusUnauthorized)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session),
		http.StatusUnauthorized)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": "ana", "password": password}, ""),
		http.StatusUnauthorized)
}

// **Unblocking gives back exactly what blocking took, which is
// nothing.**
//
// This is the whole reason the feature is not "delete and make again":
// the account keeps its password and its keys while it is shut out, so
// letting somebody back in is one request rather than an afternoon of
// re-issuing credentials on every machine they own. A block that
// revoked would be a half-undo, and an admin who blocked the wrong
// person for a minute would have cost them all of it.
func TestUnblockingRestoresTheCredentialsTheAccountAlreadyHad(t *testing.T) {
	f := servertest.New(t)
	password := add(t, f, "ana", "member")

	session := f.Login(t, "ana", password)
	key := issueKey(t, f, session)

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/ana",
		map[string]any{"blocked": true}, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/ana",
		map[string]any{"blocked": false}, f.AdminKey), http.StatusOK)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, key), http.StatusOK)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session), http.StatusOK)
}

// **Resetting a password leaves the API keys alone.**
//
// It is the one thing that separates this from revoking credentials,
// and it is invisible from the outside: both look like "the admin fixed
// their account". A forgotten password is not a lost laptop — taking
// the keys as well would turn a two-minute fix into logging back into
// every machine somebody owns, and whoever wants both has a second
// endpoint that does it.
//
// The sessions do end, because the password changed.
func TestResettingAPasswordKeepsTheKeysAndEndsTheSessions(t *testing.T) {
	f := servertest.New(t)
	password := add(t, f, "ana", "member")

	session := f.Login(t, "ana", password)
	key := issueKey(t, f, session)

	var issued struct {
		Password string `json:"password"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/users/ana/password",
		nil, f.AdminKey, &issued), http.StatusOK)
	if issued.Password == "" || issued.Password == password {
		t.Fatal("the reset handed back nothing, or the password it replaced")
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/users/me", nil, key), http.StatusOK)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session),
		http.StatusUnauthorized)

	// And the password it handed over is the one that works now.
	f.Login(t, "ana", issued.Password)
}

// **Neither of these can be aimed at yourself**, and between them and
// the last-admin count that is what keeps an instance from ending up
// with nobody who can configure it.
//
// The two refusals do almost all of the work on their own, and it is
// worth saying why: only an admin may call this, so the only way to
// target the *last* admin is to be them — at which point one of these
// two has already answered. What the count inside the transaction is
// actually for is the case no single request can show, two admins each
// taking the other's role in the same moment, which is why it is
// counted there and not here.
func TestYouCannotTakeYourOwnWayIn(t *testing.T) {
	f := servertest.New(t)
	add(t, f, "ana", "member")

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/"+f.Admin.Username,
		map[string]any{"role": "member"}, f.AdminKey), http.StatusConflict)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/"+f.Admin.Username,
		map[string]any{"blocked": true}, f.AdminKey), http.StatusConflict)

	// Still refused with a second admin on the instance. Whether there
	// is somebody to put it back is not knowable from inside the
	// request, and the mistake is the one its maker cannot undo.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/ana",
		map[string]any{"role": "admin"}, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/"+f.Admin.Username,
		map[string]any{"role": "member"}, f.AdminKey), http.StatusConflict)

	// Somebody else's role is an ordinary edit, in both directions.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/ana",
		map[string]any{"role": "member"}, f.AdminKey), http.StatusOK)
}

// Blocking, demoting and resetting a password are all an admin's.
func TestOnlyAnAdminDecidesAboutSomebodyElsesAccount(t *testing.T) {
	f := servertest.New(t)
	password := add(t, f, "ana", "member")
	add(t, f, "bo", "member")
	session := f.Login(t, "ana", password)

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/users/bo", map[string]any{"blocked": true}},
		{http.MethodPatch, "/users/bo", map[string]any{"role": "admin"}},
		{http.MethodPost, "/users/bo/password", nil},
	} {
		servertest.RequireStatus(t, f.DoAs(t, c.method, c.path, c.body, session),
			http.StatusForbidden)
	}
}
