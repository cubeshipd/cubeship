package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func repoJSON(id string, dbID int, topics ...string) map[string]any {
	nodes := []map[string]any{}
	for _, t := range topics {
		nodes = append(nodes, map[string]any{"topic": map[string]any{"name": t}})
	}
	return map[string]any{
		"id": id, "databaseId": dbID, "name": "cubeship-umami-template", "url": "https://github.com/o/r",
		"description": nil, "stargazerCount": 4, "isPrivate": false,
		"owner":            map[string]any{"login": "o", "avatarUrl": "https://avatars/o"},
		"repositoryTopics": map[string]any{"nodes": nodes},
		"releases": map[string]any{"nodes": []map[string]any{{
			"tagName": "v1", "name": nil, "url": "https://github.com/o/r/releases/tag/v1",
			"isDraft": false, "isPrerelease": false, "publishedAt": "2026-09-01T12:00:00Z",
			"createdAt": "2026-09-01T11:00:00Z", "tagCommit": map[string]any{"oid": "abc"},
		}}},
	}
}

func githubServer(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch {
		case strings.Contains(req.Query, "search(") && req.Variables["after"] == nil:
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"search": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "c1"},
				"nodes":    []any{repoJSON("R_1", 1, Topic), map[string]any{}},
			}}})
		case strings.Contains(req.Query, "search("):
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"search": map[string]any{
				"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				"nodes":    []any{repoJSON("R_2", 2, Topic)},
			}}})
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"data":   map[string]any{"nodes": []any{repoJSON("R_1", 1), nil}},
				"errors": []any{map[string]any{"type": "NOT_FOUND", "message": "Could not resolve to a node"}},
			})
		}
	})
	mux.HandleFunc("GET /repos/o/r/contents/{path}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "abc" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.PathValue("path") {
		case "template.yaml":
			io.WriteString(w, "version: 1\n")
		case "big.png":
			w.Write(make([]byte, 2048))
		case "docs":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			io.WriteString(w, "[]")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), Token: "token", API: srv.URL}
}

func TestSearchPagesThroughEveryResult(t *testing.T) {
	repos, err := githubServer(t).Search(context.Background(), Topic)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].ID != 1 || repos[1].NodeID != "R_2" {
		t.Fatalf("repos = %+v", repos)
	}
	r := repos[0]
	if r.Owner != "o" || r.Stars != 4 || r.Topics[0] != Topic || r.Description != "" {
		t.Errorf("repo = %+v", r)
	}
	rel := r.Releases[0]
	if rel.Commit != "abc" || rel.PublishedAt.Hour() != 12 || rel.Name != "" {
		t.Errorf("release = %+v", rel)
	}
}

func TestLookupTreatsNotFoundAsAnAnswer(t *testing.T) {
	found, err := githubServer(t).Lookup(context.Background(), []string{"R_1", "R_gone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := found["R_1"]; !ok || len(found) != 1 {
		t.Fatalf("found = %v", found)
	}
}

func TestFile(t *testing.T) {
	c := githubServer(t)
	ctx := context.Background()
	if b, err := c.File(ctx, "o", "r", "abc", "template.yaml", 1024); err != nil || string(b) != "version: 1\n" {
		t.Errorf("template.yaml: %q, %v", b, err)
	}
	if _, err := c.File(ctx, "o", "r", "abc", "README.md", 1024); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := c.File(ctx, "o", "r", "abc", "big.png", 1024); !errors.Is(err, ErrTooLarge) {
		t.Errorf("large file: %v", err)
	}
	if _, err := c.File(ctx, "o", "r", "abc", "docs", 1024); !errors.Is(err, ErrNotFound) {
		t.Errorf("directory: %v", err)
	}
}
