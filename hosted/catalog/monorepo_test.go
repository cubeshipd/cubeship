package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMonoSyncerReadsOneCommitAndRetainsLastGoodSnapshot(t *testing.T) {
	source, err := os.ReadFile("../../product/template/testdata/umami.yaml")
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.Replace(source, []byte("version: 1\n"), []byte("version: 1\nname: Umami\n"), 1)
	files := map[string][]byte{"template.yaml": source, "README.md": []byte("# Umami\n\nUmami is analytics.\n"), "icon.png": squarePNG(128)}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		path := "cubeship-templates-abc/umami/" + name
		if err := tw.WriteHeader(&tar.Header{Name: path, Mode: 0644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/cubeshipd/cubeship-templates/commits/main":
			fmt.Fprintf(w, `{"sha":%q,"commit":{"committer":{"date":"2026-09-01T12:00:00Z"}}}`, sha)
		case "/cubeshipd/cubeship-templates/tar.gz/" + sha:
			w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	store := newStore()
	syncer := MonoSyncer{Source: &MonoSource{HTTP: srv.Client(), API: srv.URL, Archive: srv.URL}, Store: store}
	rep, err := syncer.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Accepted != 1 || len(store.releases) != 1 || store.releases[0].Name != "Umami" {
		t.Fatalf("report=%+v releases=%+v", rep, store.releases)
	}
	if store.repos[templateID("umami")].Description != "Umami is analytics." {
		t.Fatal("README description was not read")
	}
	// Re-reading the same commit does not create another catalog entry.
	if _, err := syncer.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.releases) != 1 {
		t.Fatalf("same commit was indexed twice: %d", len(store.releases))
	}
}
