package server_test

import (
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/server/servertest"
)

// The API moved under a prefix so the dashboard could have the root.
// This is the property that makes both possible: /orgs is a page, and
// /api/orgs is the resource it lists.
func TestTheAPILivesUnderItsPrefixAndTheRootIsTheDashboard(t *testing.T) {
	f := servertest.New(t)

	// Do prefixes for us — this is the API.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/projects", nil, f.AdminKey), http.StatusOK)

	// The same path at the root is the dashboard's to name. What it
	// answers depends on whether this binary was built with one; what it
	// must never answer is the API.
	rec := f.DoRoot(t, http.MethodGet, "/projects")
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "json") {
		t.Errorf("GET /orgs answered JSON (%s) — it should be the dashboard's route", ct)
	}
}

// A wrong API call has to look like one. Falling through to the
// dashboard would answer 200 with HTML, which a client reads as a
// malformed response rather than as the 404 it is.
func TestAnUnknownAPIPathIsNotFound(t *testing.T) {
	f := servertest.New(t)

	for _, path := range []string{"/nope", "/nope", "/apps/a/b/c/d/e/f"} {
		rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s%s: %d, want 404", httpx.APIPrefix, path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "html") {
			t.Errorf("GET %s%s answered HTML; it reached the dashboard", httpx.APIPrefix, path)
		}
	}
}

// Infrastructure keeps its address: these are typed by a person or
// written into another program's configuration.
func TestInfrastructureStaysAtTheRoot(t *testing.T) {
	f := servertest.New(t)

	for _, path := range []string{"/healthz", "/openapi.json", "/docs"} {
		if rec := f.DoRoot(t, http.MethodGet, path); rec.Code != http.StatusOK {
			t.Errorf("GET %s: %d, want 200", path, rec.Code)
		}
	}
	// And they are not reachable under the prefix, which would be two
	// addresses for one thing.
	for _, path := range []string{"/healthz", "/openapi.json", "/docs"} {
		if rec := f.Do(t, http.MethodGet, path, nil, ""); rec.Code == http.StatusOK {
			t.Errorf("GET %s is also served under the API prefix", path)
		}
	}
}
