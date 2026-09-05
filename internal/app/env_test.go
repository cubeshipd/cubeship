package app_test

import (
	"net/http"
	"sync"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

type envResponse struct {
	Vars      map[string]string `json:"vars"`
	Effective []struct {
		Key    string `json:"key"`
		Value  string `json:"value"`
		Source string `json:"source"`
	} `json:"effective"`
}

func createApp(t *testing.T, f *servertest.Fixture, key, name string) {
	t.Helper()
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps", map[string]string{
		"name": name, "org": "acme", "project": "web",
	}, key), http.StatusCreated)
}

// appRef is where the fixture's apps live: every one created by
// createApp lands in acme/web/production.
func appRef(name string) string { return "/apps/acme/web/production/" + name }

func appEnv(t *testing.T, f *servertest.Fixture, key, name string) envResponse {
	t.Helper()
	var got envResponse
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, appRef(name)+"/env", nil, key, &got), http.StatusOK)
	return got
}

// The bug this exists to prevent: setting one variable used to delete
// every other one, silently, with no way to look before or after.
func TestMergingEnvKeepsVariablesYouDidNotMention(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, key, "myapp")

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"A": "1", "B": "2"}}, key), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"C": "3"}}, key), http.StatusOK)

	got := appEnv(t, f, key, "myapp")
	for k, want := range map[string]string{"A": "1", "B": "2", "C": "3"} {
		if got.Vars[k] != want {
			t.Errorf("%s is %q, want %q — setting C dropped it", k, got.Vars[k], want)
		}
	}
}

func TestMergingEnvOverwritesAndUnsets(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, key, "myapp")

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"A": "1", "B": "2", "C": "3"}}, key), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"A": "changed"}, "unset": []string{"B"}}, key), http.StatusOK)

	got := appEnv(t, f, key, "myapp")
	if got.Vars["A"] != "changed" {
		t.Errorf("A is %q, want changed", got.Vars["A"])
	}
	if _, present := got.Vars["B"]; present {
		t.Errorf("B survived being unset: %v", got.Vars)
	}
	if got.Vars["C"] != "3" {
		t.Errorf("C is %q, want 3 — it was mentioned nowhere", got.Vars["C"])
	}
}

// PUT still replaces. That is the documented behaviour and the CLI hides
// it behind an explicit "replace --yes", but the endpoint must keep doing
// what it says.
func TestPuttingEnvStillReplacesEverything(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, key, "myapp")

	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"A": "1", "B": "2"}}, key), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, appRef("myapp")+"/env",
		map[string]any{"vars": map[string]string{"C": "3"}}, key), http.StatusOK)

	got := appEnv(t, f, key, "myapp")
	if len(got.Vars) != 1 || got.Vars["C"] != "3" {
		t.Errorf("PUT should have replaced everything, got %v", got.Vars)
	}
}

// Reading an app's env is the other half of the fix: you could not see
// what was configured, which is what made losing it invisible.
func TestReadingAppEnvShowsWhereEachValueCameFrom(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, memberKey, "myapp")

	// One key set at all three levels, plus one unique to each.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/orgs/acme/projects/web/env",
		map[string]any{"set": map[string]string{"SHARED": "from-project", "ONLY_PROJECT": "p"}},
		f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/orgs/acme/projects/web/environments/production/env",
		map[string]any{"set": map[string]string{"SHARED": "from-environment", "ONLY_ENV": "e"}},
		f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"SHARED": "from-app", "ONLY_APP": "a"}},
		memberKey), http.StatusOK)

	got := appEnv(t, f, memberKey, "myapp")

	// vars is the app's own, and nothing else.
	if len(got.Vars) != 2 || got.Vars["SHARED"] != "from-app" || got.Vars["ONLY_APP"] != "a" {
		t.Errorf("vars should hold only the app's own variables, got %v", got.Vars)
	}

	effective := map[string]struct{ value, source string }{}
	for _, v := range got.Effective {
		effective[v.Key] = struct{ value, source string }{v.Value, v.Source}
	}
	for key, want := range map[string]struct{ value, source string }{
		"SHARED":       {"from-app", "app"},
		"ONLY_APP":     {"a", "app"},
		"ONLY_ENV":     {"e", "environment"},
		"ONLY_PROJECT": {"p", "project"},
	} {
		got := effective[key]
		if got.value != want.value || got.source != want.source {
			t.Errorf("%s resolved to %q from %q, want %q from %q",
				key, got.value, got.source, want.value, want.source)
		}
	}
	if len(effective) != 4 {
		t.Errorf("expected four effective variables, got %v", effective)
	}
}

// The merge happens in one SQL statement precisely so this holds. A
// read-modify-write would have each writer build its map from the value
// it read, and the last commit would drop the others' keys.
func TestConcurrentMergesDoNotLoseKeys(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)
	createApp(t, f, key, "myapp")

	const writers = 8
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := string(rune('A' + i))
			f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
				map[string]any{"set": map[string]string{name: "set"}}, key)
		}()
	}
	wg.Wait()

	got := appEnv(t, f, key, "myapp")
	if len(got.Vars) != writers {
		t.Fatalf("expected all %d concurrently written keys to survive, got %v", writers, got.Vars)
	}
}

func TestReadingEnvRequiresAccessToTheApp(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)
	createApp(t, f, memberKey, "myapp")

	if rec := f.Do(t, http.MethodGet, appRef("myapp")+"/env", nil, outsiderKey); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 so the app's existence stays hidden, got %d", rec.Code)
	}
	if rec := f.Do(t, http.MethodPatch, appRef("myapp")+"/env",
		map[string]any{"set": map[string]string{"A": "1"}}, outsiderKey); rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// Creating the same name twice must be a 409, and stay a 409 when the
// two attempts race. The old check-then-insert had both callers pass the
// lookup, and the loser surfaced as a 500 built from a driver error.
func TestConcurrentCreatesOfTheSameNameConflictCleanly(t *testing.T) {
	f := servertest.New(t)
	_, key := f.AddMember(t, "member", org.RoleMember)

	const attempts = 6
	codes := make([]int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = f.Do(t, http.MethodPost, "/apps", map[string]string{
				"name": "myapp", "org": "acme", "project": "web",
			}, key).Code
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
			t.Errorf("a racing create answered %d; only 201 and 409 are correct", code)
		}
	}
	if created != 1 {
		t.Errorf("%d creates succeeded, want exactly 1", created)
	}
	if conflicts != attempts-1 {
		t.Errorf("%d creates conflicted, want %d", conflicts, attempts-1)
	}
}
