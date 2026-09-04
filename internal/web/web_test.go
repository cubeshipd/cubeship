package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"cubeship/internal/web"
)

func built() http.Handler {
	return web.HandlerFor(fstest.MapFS{
		"index.html":                     {Data: []byte("<!doctype html>shell")},
		"login.html":                     {Data: []byte("<!doctype html>login")},
		"_next/static/chunk-abc123.js":   {Data: []byte("console.log(1)")},
		"favicon.ico":                    {Data: []byte("icon")},
		"orgs/index.html":                {Data: []byte("<!doctype html>orgs")},
		"_next/static/css/style-a1b.css": {Data: []byte("body{}")},
	})
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// A route the front-end resolves itself has no file behind it. Answering
// 404 would make every link a broken page on reload.
func TestUnknownRoutesGetTheAppShell(t *testing.T) {
	h := built()
	for _, path := range []string{"/", "/dashboard", "/apps/acme/web/production/api", "/anything/at/all"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200", path, rec.Code)
		}
		if body := rec.Body.String(); body != "<!doctype html>shell" {
			t.Errorf("GET %s served %q, want the shell", path, body)
		}
	}
}

// A file that exists is served as itself, including the .html the export
// writes for a named route.
func TestFilesAreServedBeforeTheShell(t *testing.T) {
	h := built()
	for path, want := range map[string]string{
		"/login":                        "<!doctype html>login",
		"/orgs":                         "<!doctype html>orgs",
		"/favicon.ico":                  "icon",
		"/_next/static/chunk-abc123.js": "console.log(1)",
	} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200", path, rec.Code)
		}
		if got := rec.Body.String(); got != want {
			t.Errorf("GET %s served %q, want %q", path, got, want)
		}
	}
}

// A missing asset must not come back as HTML: a script tag answered with
// the shell fails as a syntax error somewhere else entirely.
func TestAMissingAssetIsNotFound(t *testing.T) {
	h := built()
	for _, path := range []string{"/_next/static/gone.js", "/missing.css", "/nope.png"} {
		if rec := get(t, h, path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", path, rec.Code)
		}
	}
}

// The shell must be revalidated and the fingerprinted bundles must not
// be. Backwards, a browser keeps running the previous release.
func TestCaching(t *testing.T) {
	h := built()
	if got := get(t, h, "/_next/static/chunk-abc123.js").Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("hashed bundle Cache-Control is %q", got)
	}
	for _, path := range []string{"/", "/login", "/favicon.ico"} {
		if got := get(t, h, path).Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s Cache-Control is %q, want no-cache", path, got)
		}
	}
}

// A daemon compiled without a dashboard says so. A blank 404 at the
// address the installer tells you to open reads like a broken install.
func TestAnUnbuiltDashboardExplainsItself(t *testing.T) {
	h := web.HandlerFor(fstest.MapFS{})
	rec := get(t, h, "/")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "make web") {
		t.Errorf("the message does not say how to build one: %q", body)
	}
}

// The dashboard is mounted for every method, because the root pattern
// cannot be method-scoped without becoming ambiguous with the API's.
// Static files answer reads and nothing else.
func TestWritesAreRejected(t *testing.T) {
	h := built()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /: %d, want 405", method, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("%s /: Allow is %q", method, allow)
		}
	}
}
