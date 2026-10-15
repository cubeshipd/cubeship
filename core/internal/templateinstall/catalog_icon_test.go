package templateinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonorepoIconUsesNegativeCatalogID(t *testing.T) {
	sha := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/icons/-42/"+sha+".png" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("png"))
	}))
	defer srv.Close()
	c := NewHTTPCatalog(srv.URL, "")
	if !iconRepository.MatchString(srv.URL + "/icons/-42/" + sha + ".png") {
		t.Fatal("project icon parser refused a monorepo ID")
	}
	got, err := c.Icon(context.Background(), "-42", sha+".png")
	if err != nil || string(got) != "png" {
		t.Fatalf("icon=%q err=%v", got, err)
	}
}
