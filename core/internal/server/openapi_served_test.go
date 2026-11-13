package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/platform/httpx"
	"cubeship/internal/server"
	"cubeship/internal/server/servertest"
)

// The reference page's "try it" has to target a real address. It used to
// advertise the literal template "https://api.{domain}", which Scalar
// renders as a field the reader has to fill in by hand.
func TestServedDocumentTargetsTheAddressItWasFetchedFrom(t *testing.T) {
	f := servertest.New(t)

	type serverEntry struct {
		URL         string `json:"url"`
		Description string `json:"description"`
	}
	fetch := func(t *testing.T, mutate func(*http.Request)) []serverEntry {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, server.OpenAPIPath, nil)
		if mutate != nil {
			mutate(req)
		}
		rec := httptest.NewRecorder()
		f.Server.Router().ServeHTTP(rec, req)
		servertest.RequireStatus(t, rec, http.StatusOK)

		var doc struct {
			Servers []serverEntry `json:"servers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("decode document: %v", err)
		}
		if len(doc.Servers) == 0 {
			t.Fatal("the document offers no server at all")
		}
		for _, s := range doc.Servers {
			if strings.ContainsAny(s.URL, "{}") {
				t.Errorf("server URL %q still carries a placeholder", s.URL)
			}
			// The paths in the document are the ones the modules
			// register; the prefix that says where they live is here.
			if !strings.HasSuffix(s.URL, httpx.APIPrefix) {
				t.Errorf("server URL %q does not end in %s, so every path in the document is wrong",
					s.URL, httpx.APIPrefix)
			}
		}
		return doc.Servers
	}

	t.Run("plain HTTP, as over an SSH tunnel", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) { r.Host = "127.0.0.1:9000" })
		if servers[0].URL != "http://127.0.0.1:9000"+httpx.APIPrefix {
			t.Errorf("first server is %q, want the address the request arrived on", servers[0].URL)
		}
		// The configured public address is still offered as an alternative.
		if len(servers) != 2 || servers[1].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("expected the canonical address as a second option, got %v", servers)
		}
	})

	t.Run("through Traefik, which terminates TLS", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) {
			r.Host = servertest.APIHost
			r.Header.Set("X-Forwarded-Proto", "https")
		})
		if servers[0].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("first server is %q, want https://%s%s", servers[0].URL, servertest.APIHost, httpx.APIPrefix)
		}
		// Origin and canonical coincide, so it is offered once, not twice.
		if len(servers) != 1 {
			t.Errorf("expected a single server entry, got %v", servers)
		}
	})

	t.Run("a chained proxy appends to X-Forwarded-Proto", func(t *testing.T) {
		servers := fetch(t, func(r *http.Request) {
			r.Host = servertest.APIHost
			r.Header.Set("X-Forwarded-Proto", "https, http")
		})
		if servers[0].URL != "https://"+servertest.APIHost+httpx.APIPrefix {
			t.Errorf("first server is %q; only the client-facing scheme counts", servers[0].URL)
		}
	})
}
