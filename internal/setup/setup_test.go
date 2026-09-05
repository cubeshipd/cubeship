package setup_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

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

// orgSlug is what the onboarding screen asks for after the account: the
// first organization's name, typed rather than invented.
const orgSlug = "acme"

func claim(t *testing.T, f *servertest.Fixture, username string) *http.Cookie {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/setup",
		map[string]string{"username": username, "password": password, "org": orgSlug}, "")
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
		Username     string `json:"username"`
		IsSuperAdmin bool   `json:"is_super_admin"`
	}
	rec := f.DoAs(t, http.MethodGet, "/users/me", nil, session)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "lucas" || !me.IsSuperAdmin {
		t.Fatalf("the first account is %+v; it should be a super-admin", me)
	}

	// And the organization they named is there.
	var orgs []struct{ Slug string }
	rec = f.DoAs(t, http.MethodGet, "/orgs", nil, session)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &orgs); err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].Slug != orgSlug {
		t.Fatalf("expected exactly the organization that was named, got %v", orgs)
	}

	// And nothing else. A project invented here would be a row nobody
	// asked for, under a slug that can never be changed.
	var projects []struct{ Slug string }
	rec = f.DoAs(t, http.MethodGet, "/orgs/"+orgSlug+"/projects", nil, session)
	servertest.RequireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("setup created projects: %v", projects)
	}
}

// Setup is not a way to add users. Once the instance is claimed it is
// closed, or anyone who could reach the port could add themselves.
func TestSetupClosesAfterTheFirstAccount(t *testing.T) {
	f := servertest.NewEmpty(t)
	claim(t, f, "lucas")

	rec := f.Do(t, http.MethodPost, "/setup",
		map[string]string{"username": "intruder", "password": password, "org": "other"}, "")
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
		map[string]string{"username": "intruder", "password": password, "org": "other"}, ""), http.StatusConflict)
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
				"org":      "org" + string(rune('a'+i)),
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

// A rejected claim must leave nothing behind. A user row with no
// organization would be unrecoverable: setup refuses to run again, and
// the account it created has nowhere to work.
func TestARefusedClaimLeavesNothingBehind(t *testing.T) {
	f := servertest.NewEmpty(t)

	for _, body := range []map[string]string{
		{"username": "lucas", "password": "short", "org": orgSlug},
		{"username": "", "password": password, "org": orgSlug},
		{"username": "lucas", "password": "", "org": orgSlug},
		{"username": "Not A Slug", "password": password, "org": orgSlug},
		{"username": "lucas", "password": password, "org": ""},
		{"username": "lucas", "password": password, "org": "Not A Slug"},
		{"username": "lucas", "password": password, "org": "settings"},
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
