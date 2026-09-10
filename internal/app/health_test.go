package app_test

import (
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/server/servertest"
)

// The value is interpolated into a Traefik dynamic YAML document *and*
// into a container label, so a quote, a newline or a colon in it is
// configuration somebody else wrote. Same reasoning as ValidHost, and
// the same answer: the grammar is the rule.
func TestAHealthPathIsCheckedBeforeItReachesAProxy(t *testing.T) {
	for _, path := range []string{"/healthz", "/", "/api/v1/health", "/_health-check.json", ""} {
		if !app.ValidHealthPath(path) {
			t.Errorf("%q is a path an app may answer on, and it was refused", path)
		}
	}
	for _, path := range []string{
		"healthz",               // no leading slash: Traefik would check something else
		"/health z",             // a space
		"/health\npath: /admin", // a second YAML key
		`/health"`,              // closes the quoted string it lands in
		"/health\\",             // escapes it instead
		"/health?deep=1",        // a query, whose meaning here is a guess
		"/health#frag",          //
		"/" + strings.Repeat("a", app.MaxHealthPathLength),
	} {
		if app.ValidHealthPath(path) {
			t.Errorf("%q reached a proxy's configuration", path)
		}
	}
}

// And the refusal is the API's, not only the package's — a rule that
// lives below the surface has to be visible through it.
func TestTheAPIRefusesAHealthPathItCannotHandOver(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "api")

	rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"health_path": "healthz"}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a path with no leading slash: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/") {
		t.Errorf("the refusal is %q, and it has to say what the rule is", rec.Body.String())
	}

	// And a real one is kept, and comes back.
	var updated struct {
		HealthPath string `json:"health_path"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"health_path": "/healthz"}, f.AdminKey, &updated), http.StatusOK)
	if updated.HealthPath != "/healthz" {
		t.Errorf("the app reports %q as its health path", updated.HealthPath)
	}

	// Clearing it is how you turn the check off, so an empty string has
	// to be a value rather than "leave it alone".
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"health_path": ""}, f.AdminKey, &updated), http.StatusOK)
	if updated.HealthPath != "" {
		t.Errorf("the check could not be turned off: %q", updated.HealthPath)
	}
}
