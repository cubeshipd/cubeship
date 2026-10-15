package templateinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSourceSupportsBothRepositoryGenerations(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "legacy"}[legacy], func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if legacy && strings.HasPrefix(r.URL.Path, "/cubeshipd/cubeship/") {
					http.NotFound(w, r)
					return
				}
				w.Write([]byte("version: 1"))
			}))
			defer server.Close()
			c := NewHTTPCatalog("", "")
			c.Raw = server.URL
			c.HTTP = server.Client()
			source, err := c.Source(context.Background(), "cubeshipd", "umami", "abc1234")
			if err != nil || string(source) != "version: 1" {
				t.Fatalf("source=%s err=%v", source, err)
			}
			if paths[0] != "/cubeshipd/cubeship/abc1234/templates/umami/template.yaml" {
				t.Fatal(paths)
			}
			if legacy && (len(paths) != 2 || paths[1] != "/cubeshipd/cubeship-templates/abc1234/umami/template.yaml") {
				t.Fatal(paths)
			}
			count := len(paths)
			if _, err := c.Source(context.Background(), "cubeshipd", "../bad", "abc1234"); err == nil || len(paths) != count {
				t.Fatal("invalid path reached GitHub")
			}
		})
	}
}

func TestSourceDoesNotFallbackOnGitHubFailure(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	c := NewHTTPCatalog("", "")
	c.Raw = server.URL
	if _, err := c.Source(context.Background(), "cubeshipd", "umami", "abc1234"); err == nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
