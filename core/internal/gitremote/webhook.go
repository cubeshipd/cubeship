package gitremote

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cubeship/internal/platform/database"
)

type changedCommit struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Removed  []string `json:"removed"`
}

// Receive verifies the configured connection before trusting a pushed repository or ref.
func (s *Service) Receive(ctx context.Context, id int64, body []byte, header http.Header) (*Push, error) {
	c, err := s.Repo().Get(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrWebhook
	}
	if err != nil {
		return nil, err
	}
	if c.secret == "" {
		return nil, ErrWebhook
	}
	if c.Provider == GitLab {
		if subtle.ConstantTimeCompare([]byte(header.Get("X-Gitlab-Token")), []byte(c.secret)) != 1 {
			return nil, ErrWebhook
		}
		if header.Get("X-Gitlab-Event") != "Push Hook" {
			return nil, nil
		}
	} else {
		mac := hmac.New(sha256.New, []byte(c.secret))
		mac.Write(body)
		got, err := hex.DecodeString(header.Get("X-Gitea-Signature"))
		if err != nil || !hmac.Equal(got, mac.Sum(nil)) {
			return nil, ErrWebhook
		}
		if header.Get("X-Gitea-Event") != "push" {
			return nil, nil
		}
	}
	var payload struct {
		ObjectKind string `json:"object_kind"`
		Ref        string `json:"ref"`
		After      string `json:"after"`
		Project    struct {
			GitHTTPURL string `json:"git_http_url"`
		} `json:"project"`
		Repository struct {
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
		Commits           []changedCommit `json:"commits"`
		TotalCommitsCount int             `json:"total_commits_count"`
		TotalCommits      int             `json:"total_commits"`
	}
	if json.Unmarshal(body, &payload) != nil || c.Provider == GitLab && payload.ObjectKind != "push" {
		return nil, ErrWebhook
	}
	branch, ok := strings.CutPrefix(payload.Ref, "refs/heads/")
	if !ok || branch == "" {
		return nil, nil
	}
	sha, err := hex.DecodeString(payload.After)
	if err != nil || len(sha) != sha1.Size {
		return nil, ErrWebhook
	}
	allZero := true
	for _, b := range sha {
		allZero = allZero && b == 0
	}
	if allZero {
		return nil, nil
	}
	raw := payload.Repository.CloneURL
	if c.Provider == GitLab {
		raw = payload.Project.GitHTTPURL
	}
	path, ok := repositoryPath(c.BaseURL, raw)
	if !ok {
		return nil, ErrWebhook
	}
	repo, err := s.repository(ctx, c, path)
	if errors.Is(err, ErrRepo) {
		return nil, ErrWebhook
	}
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSuffix(raw, ".git"), strings.TrimSuffix(repo.URL, ".git")) {
		return nil, ErrWebhook
	}
	total := payload.TotalCommits
	if c.Provider == GitLab {
		total = payload.TotalCommitsCount
	}
	changed := make([]string, 0)
	seen := map[string]bool{}
	for _, commit := range payload.Commits {
		for _, path := range append(append(commit.Added, commit.Modified...), commit.Removed...) {
			if !seen[path] {
				seen[path] = true
				changed = append(changed, path)
			}
		}
	}
	return &Push{RepositoryURL: repo.URL, Branch: branch, SHA: payload.After, Changed: changed, Complete: total > 0 && total == len(payload.Commits)}, nil
}
