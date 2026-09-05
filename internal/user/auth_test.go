package user_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

const goodPassword = "correct horse battery staple"

// withPassword gives the fixture's super-admin one, which it does not
// have: it was bootstrapped with an API key.
func withPassword(t *testing.T, f *servertest.Fixture) {
	t.Helper()
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/users/me/password",
		map[string]string{"new_password": goodPassword}, f.AdminKey), http.StatusOK)
}

// The whole point of C: a browser can hold a session and use it on the
// same endpoints an API key reaches.
func TestSigningInGivesASessionThatWorksEverywhere(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)

	session := f.Login(t, "admin", goodPassword)
	if session.Value == "" {
		t.Fatal("the sign-in returned an empty session")
	}

	var me struct {
		Username string `json:"username"`
	}
	rec := f.DoAs(t, http.MethodGet, "/users/me", nil, session)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "admin" {
		t.Errorf("the session resolved to %q", me.Username)
	}

	// And on an ordinary resource, not just the identity endpoint.
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/projects", nil, session), http.StatusOK)
}

// The cookie has to survive a fresh install reached at http://<ip>:3000.
// A Secure cookie there is never sent back, so the sign-in would appear
// to work and nothing would stay signed in.
func TestSessionCookieIsUsableOverPlainHTTP(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)

	session := f.Login(t, "admin", goodPassword)
	if session.Secure {
		t.Error("the cookie is Secure over plain HTTP, so a browser would never send it back")
	}
	if !session.HttpOnly {
		t.Error("the cookie is reachable from JavaScript")
	}
	if session.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite is %v; Lax is what stands in for CSRF tokens here", session.SameSite)
	}
}

// A wrong password and an unknown username must be indistinguishable —
// otherwise sign-in answers "does this account exist".
func TestFailedSignInsAreIndistinguishable(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)

	wrong := f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": "admin", "password": "not the password"}, "")
	unknown := f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": "nobody", "password": goodPassword}, "")

	servertest.RequireStatus(t, wrong, http.StatusUnauthorized)
	servertest.RequireStatus(t, unknown, http.StatusUnauthorized)
	if wrong.Body.String() != unknown.Body.String() {
		t.Errorf("the two failures read differently:\n  wrong password: %q\n  unknown user:   %q",
			wrong.Body.String(), unknown.Body.String())
	}
}

// An account created by an organization admin has an API key and no
// password. It must not be possible to sign in as one — least of all
// with an empty password.
func TestAnAccountWithNoPasswordCannotSignIn(t *testing.T) {
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/users",
		map[string]string{"username": "employee", "role": "member"}, f.AdminKey), http.StatusCreated)

	for _, password := range []string{"", " ", goodPassword} {
		rec := f.Do(t, http.MethodPost, "/auth/login",
			map[string]string{"username": "employee", "password": password}, "")
		servertest.RequireStatus(t, rec, http.StatusUnauthorized)
	}
}

func TestLoggingOutEndsTheSession(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)
	session := f.Login(t, "admin", goodPassword)

	servertest.RequireStatus(t, f.DoAs(t, http.MethodPost, "/auth/logout", nil, session), http.StatusOK)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session), http.StatusUnauthorized)
}

// Changing a password ends every other session: whoever knew the old one
// should not stay signed in.
func TestChangingThePasswordEndsEveryOtherSession(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)

	elsewhere := f.Login(t, "admin", goodPassword)
	here := f.Login(t, "admin", goodPassword)

	const next = "an entirely different passphrase"
	servertest.RequireStatus(t, f.DoAs(t, http.MethodPut, "/users/me/password",
		map[string]string{"current_password": goodPassword, "new_password": next}, here), http.StatusOK)

	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, elsewhere), http.StatusUnauthorized)
	// The session that made the change survives — locking yourself out by
	// changing your own password would be absurd.
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, here), http.StatusOK)

	// And the new password is the one that works.
	f.Login(t, "admin", next)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": "admin", "password": goodPassword}, ""), http.StatusUnauthorized)
}

// An account that already has a password must prove it knows it. A
// borrowed terminal, or a stolen session, should not be enough to lock
// the owner out of their own account.
func TestChangingAPasswordNeedsTheCurrentOne(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)
	session := f.Login(t, "admin", goodPassword)

	rec := f.DoAs(t, http.MethodPut, "/users/me/password",
		map[string]string{"current_password": "wrong", "new_password": "something else entirely"}, session)
	servertest.RequireStatus(t, rec, http.StatusForbidden)

	// The original still works.
	f.Login(t, "admin", goodPassword)
}

func TestShortPasswordsAreRefused(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPut, "/users/me/password",
		map[string]string{"new_password": "short"}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "at least") {
		t.Errorf("the refusal does not say what is wrong: %q", rec.Body.String())
	}
}

// An expired session resolves to nobody, without waiting for the
// housekeeping pass that deletes the row.
func TestAnExpiredSessionIsRejected(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)
	session := f.Login(t, "admin", goodPassword)
	ctx := context.Background()

	if _, err := f.DB.ExecContext(ctx,
		`UPDATE sessions SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("age the session: %v", err)
	}
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/users/me", nil, session), http.StatusUnauthorized)

	// Housekeeping then removes it.
	n, err := f.Server.Users.PurgeExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("PurgeExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("purged %d rows, want 1", n)
	}
}

// A database someone gets a copy of must not hand them live sessions.
func TestOnlyTheSessionHashIsStored(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)
	session := f.Login(t, "admin", goodPassword)

	var stored string
	if err := f.DB.QueryRowContext(context.Background(),
		`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatalf("read the session row: %v", err)
	}
	if stored == session.Value {
		t.Fatal("the session token itself is in the database")
	}
}

// Passwords are hashed with Argon2id, and the parameters travel with the
// hash so raising them later leaves existing hashes verifiable.
func TestPasswordHashesCarryTheirParameters(t *testing.T) {
	encoded, err := user.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$") {
		t.Fatalf("hash is not Argon2id: %q", encoded)
	}
	if strings.Contains(encoded, goodPassword) {
		t.Fatal("the password is in its own hash")
	}
	if !user.VerifyPassword(encoded, goodPassword) {
		t.Error("a hash does not verify its own password")
	}
	if user.VerifyPassword(encoded, goodPassword+"x") {
		t.Error("a wrong password verified")
	}

	// Two hashes of the same password differ: each carries its own salt.
	other, err := user.HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if other == encoded {
		t.Error("two hashes of the same password are identical, so there is no salt")
	}
}

// A key and a session reach the same place, and a request carrying both
// is treated as the key — the explicit credential wins.
func TestAPIKeysStillWorkAlongsideSessions(t *testing.T) {
	f := servertest.New(t)
	withPassword(t, f)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects", nil, f.AdminKey), http.StatusOK)

	session := f.Login(t, "admin", goodPassword)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/projects", nil, session), http.StatusOK)
}

func TestUnauthenticatedRequestsAreStillRejected(t *testing.T) {
	f := servertest.New(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects", nil, ""), http.StatusUnauthorized)
	servertest.RequireStatus(t, f.DoAs(t, http.MethodGet, "/projects", nil, &http.Cookie{
		Name: user.SessionCookieName, Value: "not-a-real-session",
	}), http.StatusUnauthorized)
}
