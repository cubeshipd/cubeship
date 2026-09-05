package setup_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/server/servertest"
	"cubeship/internal/setup"
	"cubeship/internal/user"
)

const password = "correct horse battery staple"

func status(t *testing.T, f *servertest.Fixture) setup.Status {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/setup", nil, "")
	servertest.RequireStatus(t, rec, http.StatusOK)

	var got setup.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode setup status: %v", err)
	}
	return got
}

func claim(t *testing.T, f *servertest.Fixture, username string) *http.Cookie {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/setup",
		map[string]string{"username": username, "password": password}, "")
	servertest.RequireStatus(t, rec, http.StatusCreated)

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == user.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("setup did not sign the new account in")
	return nil
}

// The flow end to end: a fresh instance says it needs setting up, one
// request claims it, and whoever made that request is already signed in,
// in the organization they named.
func TestClaimingAFreshInstance(t *testing.T) {
	f := servertest.NewEmpty(t)

	if !status(t, f).Needed {
		t.Fatal("a fresh instance does not report needing setup")
	}

	session := claim(t, f, "lucas")

	if status(t, f).Needed {
		t.Error("the instance still reports needing setup after being claimed")
	}

	// Signed in, without having to type the password a second time.
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	rec := f.DoAs(t, http.MethodGet, "/users/me", nil, session)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "lucas" || me.Role != "admin" {
		t.Fatalf("the first account is %+v; it should be an admin", me)
	}

}

// Setup is not a way to add users. Once the instance is claimed it is
// closed, or anyone who could reach the port could add themselves.
func TestSetupClosesAfterTheFirstAccount(t *testing.T) {
	f := servertest.NewEmpty(t)
	claim(t, f, "lucas")

	rec := f.Do(t, http.MethodPost, "/setup",
		map[string]string{"username": "intruder", "password": password}, "")
	servertest.RequireStatus(t, rec, http.StatusConflict)
}

// A fixture that already has an account is an instance someone claimed,
// and setup must be shut on it too.
func TestSetupIsClosedOnAnInstanceThatAlreadyHasUsers(t *testing.T) {
	f := servertest.New(t)

	if status(t, f).Needed {
		t.Error("an instance with an account reports needing setup")
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/setup",
		map[string]string{"username": "intruder", "password": password}, ""), http.StatusConflict)
}

// Two people opening the page at once must not both claim it. The
// username's unique index does not help — they may well pick different
// names — so setup takes an advisory lock and the loser is refused.
func TestConcurrentClaimsProduceOneAccount(t *testing.T) {
	f := servertest.NewEmpty(t)

	const attempts = 6
	codes := make([]int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = f.Do(t, http.MethodPost, "/setup", map[string]string{
				"username": "claimant" + string(rune('a'+i)),
				"password": password,
			}, "").Code
		}()
	}
	wg.Wait()

	created, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("a racing claim answered %d; only 201 and 409 are correct", code)
		}
	}
	if created != 1 {
		t.Errorf("%d claims succeeded, want exactly 1", created)
	}
	if conflicts != attempts-1 {
		t.Errorf("%d claims conflicted, want %d", conflicts, attempts-1)
	}
}

// A rejected claim must leave nothing behind. Setup refuses to run
// again once an account exists, so a half-made one would be
// unrecoverable.
func TestARefusedClaimLeavesNothingBehind(t *testing.T) {
	f := servertest.NewEmpty(t)

	for _, body := range []map[string]string{
		{"username": "lucas", "password": "short"},
		{"username": "", "password": password},
		{"username": "lucas", "password": ""},
		{"username": "Not A Slug", "password": password},
	} {
		rec := f.Do(t, http.MethodPost, "/setup", body, "")
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
	}

	if !status(t, f).Needed {
		t.Fatal("a refused claim left the instance looking claimed")
	}
	// And it can still be claimed properly.
	claim(t, f, "lucas")
}

// The password given at setup is the one that signs in afterwards.
func TestTheSetupPasswordWorks(t *testing.T) {
	f := servertest.NewEmpty(t)
	claim(t, f, "lucas")

	f.Login(t, "lucas", password)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": "lucas", "password": "something else"}, ""), http.StatusUnauthorized)
}

// The unclaimed-Portainer problem: the installer publishes a port, and
// until somebody creates the first account, whoever reaches that port
// first is the admin of the machine. On a VPS with a public address that
// is a race the operator can lose between running the installer and
// opening their browser.
//
// The token is what closes it. It is written where only root can read,
// so claiming the instance takes access to the host rather than a lucky
// HTTP request.
func TestClaimingNeedsTheSetupToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), setup.TokenFileName)
	if err := os.WriteFile(path, []byte("the-real-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := servertest.NewUnclaimed(t, setup.Token{Value: "the-real-token", Path: path})

	if got := status(t, f); !got.Needed || !got.TokenRequired {
		t.Fatalf("a token-guarded instance reports %+v", got)
	}

	for _, token := range []string{"", "not-the-token"} {
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/setup",
			map[string]string{"username": "lucas", "password": password, "token": token}, ""),
			http.StatusForbidden)
	}

	// Nothing was created by any of that.
	if !status(t, f).Needed {
		t.Fatal("a refused claim set the instance up anyway")
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/setup", map[string]string{
		"username": "lucas", "password": password, "token": "the-real-token",
	}, ""), http.StatusCreated)

	// Spent: it can claim nothing now, so it does not stay on disk.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the setup token is still on disk after the instance was claimed: %v", err)
	}
}

// The token the installer printed has to be the token the daemon still
// asks for, so a restart must not mint a new one — and an instance with
// an account has no use for one at all.
func TestTheSetupTokenLastsUntilTheInstanceIsClaimed(t *testing.T) {
	db := dbtest.New(t)
	dir := t.TempDir()
	ctx := context.Background()

	first, err := setup.EnsureToken(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Required() {
		t.Fatal("an unclaimed instance got no setup token")
	}

	again, err := setup.EnsureToken(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.Value != first.Value {
		t.Error("a restart minted a new token, so the one the installer printed stopped working")
	}

	servertest.CreateUser(t, db, "lucas", user.RoleAdmin)
	claimed, err := setup.EnsureToken(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Required() {
		t.Error("an instance with an account still asks for a setup token")
	}
	if _, err := os.Stat(first.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a token left from before the claim was not cleaned up: %v", err)
	}
}
