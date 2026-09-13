package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client reads GitHub: GraphQL for repositories and releases, so a page
// of fifty repositories and their releases is one request, and the REST
// contents endpoint for a file at a commit.
type Client struct {
	HTTP  *http.Client
	Token string
	// API is https://api.github.com, or a test server.
	API string
}

const repoFields = `
  id databaseId name url description stargazerCount isPrivate
  owner { login avatarUrl }
  repositoryTopics(first: 20) { nodes { topic { name } } }
  releases(first: 10, orderBy: {field: CREATED_AT, direction: DESC}) {
    nodes { tagName name url isDraft isPrerelease publishedAt createdAt tagCommit { oid } }
  }`

const searchQuery = `query($q: String!, $after: String) {
  search(query: $q, type: REPOSITORY, first: 50, after: $after) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on Repository {` + repoFields + ` } }
  }
}`

const lookupQuery = `query($ids: [ID!]!) {
  nodes(ids: $ids) { ... on Repository {` + repoFields + ` } }
}`

type gqlRepo struct {
	ID             string  `json:"id"`
	DatabaseID     int64   `json:"databaseId"`
	Name           string  `json:"name"`
	URL            string  `json:"url"`
	Description    *string `json:"description"`
	StargazerCount int     `json:"stargazerCount"`
	IsPrivate      bool    `json:"isPrivate"`
	Owner          struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"owner"`
	RepositoryTopics struct {
		Nodes []struct {
			Topic struct {
				Name string `json:"name"`
			} `json:"topic"`
		} `json:"nodes"`
	} `json:"repositoryTopics"`
	Releases struct {
		Nodes []struct {
			TagName      string     `json:"tagName"`
			Name         *string    `json:"name"`
			URL          string     `json:"url"`
			IsDraft      bool       `json:"isDraft"`
			IsPrerelease bool       `json:"isPrerelease"`
			PublishedAt  *time.Time `json:"publishedAt"`
			CreatedAt    time.Time  `json:"createdAt"`
			TagCommit    *struct {
				OID string `json:"oid"`
			} `json:"tagCommit"`
		} `json:"nodes"`
	} `json:"releases"`
}

func (g gqlRepo) repo() Repo {
	r := Repo{
		NodeID: g.ID, ID: g.DatabaseID, Owner: g.Owner.Login, Name: g.Name,
		OwnerAvatar: g.Owner.AvatarURL, URL: g.URL, Stars: g.StargazerCount, Private: g.IsPrivate,
		Topics: []string{},
	}
	if g.Description != nil {
		r.Description = *g.Description
	}
	for _, t := range g.RepositoryTopics.Nodes {
		r.Topics = append(r.Topics, t.Topic.Name)
	}
	for _, n := range g.Releases.Nodes {
		rel := Release{Tag: n.TagName, URL: n.URL, Draft: n.IsDraft, Prerelease: n.IsPrerelease, PublishedAt: n.CreatedAt}
		if n.Name != nil {
			rel.Name = *n.Name
		}
		if n.PublishedAt != nil {
			rel.PublishedAt = *n.PublishedAt
		}
		if n.TagCommit != nil {
			rel.Commit = n.TagCommit.OID
		}
		r.Releases = append(r.Releases, rel)
	}
	return r
}

type gqlError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (c *Client) graphql(ctx context.Context, query string, vars map[string]any, out any) ([]gqlError, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.API+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d: %s", res.StatusCode, snippet(raw))
	}
	envelope := struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("read GitHub's answer: %w", err)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return envelope.Errors, fmt.Errorf("GitHub answered no data: %s", snippet(raw))
	}
	return envelope.Errors, json.Unmarshal(envelope.Data, out)
}

// Search pages through every repository with the topic. GitHub's search
// stops at a thousand results, which is a problem worth having.
func (c *Client) Search(ctx context.Context, topic string) ([]Repo, error) {
	var repos []Repo
	var after *string
	for {
		var data struct {
			Search struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []gqlRepo `json:"nodes"`
			} `json:"search"`
		}
		errs, err := c.graphql(ctx, searchQuery, map[string]any{"q": "topic:" + topic, "after": after}, &data)
		if err != nil {
			return nil, err
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("GitHub: %s", errs[0].Message)
		}
		for _, n := range data.Search.Nodes {
			if n.ID != "" {
				repos = append(repos, n.repo())
			}
		}
		if !data.Search.PageInfo.HasNextPage {
			return repos, nil
		}
		cursor := data.Search.PageInfo.EndCursor
		after = &cursor
	}
}

// Lookup reads repositories by node id, fifty at a time. GitHub answers
// an id that no longer resolves with a null node and a NOT_FOUND error
// beside the rest of the data, which is an answer rather than a failure.
func (c *Client) Lookup(ctx context.Context, ids []string) (map[string]Repo, error) {
	out := map[string]Repo{}
	for start := 0; start < len(ids); start += 50 {
		batch := ids[start:min(start+50, len(ids))]
		var data struct {
			Nodes []*gqlRepo `json:"nodes"`
		}
		errs, err := c.graphql(ctx, lookupQuery, map[string]any{"ids": batch}, &data)
		if err != nil {
			return nil, err
		}
		for _, e := range errs {
			if e.Type != "NOT_FOUND" {
				return nil, fmt.Errorf("GitHub: %s", e.Message)
			}
		}
		for _, n := range data.Nodes {
			if n != nil && n.ID != "" {
				out[n.ID] = n.repo()
			}
		}
	}
	return out, nil
}

// File reads one file at one commit.
func (c *Client) File(ctx context.Context, owner, name, commit, path string, limit int64) ([]byte, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", c.API,
		url.PathEscape(owner), url.PathEscape(name), url.PathEscape(path), url.QueryEscape(commit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github.raw+json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case res.StatusCode != http.StatusOK:
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("GitHub answered %d for %s: %s", res.StatusCode, path, snippet(raw))
	case res.ContentLength > limit:
		return nil, ErrTooLarge
	}
	// A directory at that path answers with a JSON listing, not a file.
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		return nil, ErrNotFound
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
