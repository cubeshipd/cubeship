package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/web"
)

// A page request reaches the dashboard's server unchanged. The daemon
// is a proxy in front of it, not a router: what a path means is the
// dashboard's business, and a daemon that rewrote paths would be a
// second place every route has to be registered.
func TestPageRequestsReachTheDashboard(t *testing.T) {
	var got *http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte("dashboard"))
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	web.Handler(strings.TrimPrefix(upstream.URL, "http://")).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dns/zones?id=4", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status is %d, want 200", rec.Code)
	}
	if got == nil {
		t.Fatal("the request never reached the dashboard")
	}
	if got.URL.Path != "/dns/zones" {
		t.Errorf("path arrived as %q, want /dns/zones", got.URL.Path)
	}
	if got.URL.RawQuery != "id=4" {
		t.Errorf("query arrived as %q, want id=4", got.URL.RawQuery)
	}
}

// A dashboard that is not up must not read as a broken install. The
// address it answers at is the one the installer tells you to open, so
// a bare 502 there is the worst thing it could say.
func TestADownDashboardExplainsItself(t *testing.T) {
	rec := httptest.NewRecorder()
	// Port 1 answers nothing anywhere.
	web.Handler("127.0.0.1:1").
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status is %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cubeship-frontend") {
		t.Errorf("the answer does not name the container to look at:\n%s", body)
	}
	if !strings.Contains(body, "/api") {
		t.Errorf("the answer does not say the API is unaffected:\n%s", body)
	}
}
