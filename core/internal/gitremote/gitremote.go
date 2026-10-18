// Package gitremote connects GitLab and Gitea instances through their token APIs.
package gitremote

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

type Provider string

const (
	GitLab Provider = "gitlab"
	Gitea  Provider = "gitea"
)

func (p Provider) Valid() bool { return p == GitLab || p == Gitea }

var (
	ErrProvider = errors.New("choose GitLab or Gitea")
	ErrURL      = errors.New("enter an HTTPS instance URL without credentials, query or fragment")
	ErrToken    = errors.New("the Git provider rejected this token or its permissions")
	ErrRemote   = errors.New("the Git provider request failed")
	ErrRepo     = errors.New("repository is unavailable to this connection")
	ErrWebhook  = errors.New("invalid webhook delivery")
)

type Connection struct {
	ID        int64     `json:"id"`
	Provider  Provider  `json:"provider"`
	BaseURL   string    `json:"base_url"`
	Account   string    `json:"account"`
	UserID    int64     `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	accountID int64
	token     string
	secret    string
}

type Repository struct {
	FullName      string   `json:"full_name"`
	URL           string   `json:"url"`
	Provider      Provider `json:"provider"`
	Private       bool     `json:"private"`
	DefaultBranch string   `json:"default_branch"`
}

type Branch struct {
	Name string `json:"name"`
}

type Push struct {
	RepositoryURL string
	Branch        string
	SHA           string
	Changed       []string
	Complete      bool
}

func normalizeBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", ErrURL
	}
	if strings.Contains(u.Path, "..") || strings.Contains(u.Path, "//") {
		return "", ErrURL
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// repositoryPath keeps URL comparison on the origin and configured base path.
// A repository needs at least owner/name after the base (nested GitLab groups are allowed).
func repositoryPath(base, raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return "", false
	}
	b, err := url.Parse(base)
	if err != nil || !strings.EqualFold(u.Host, b.Host) || !strings.HasPrefix(u.Path, strings.TrimRight(b.Path, "/")+"/") {
		return "", false
	}
	path := strings.TrimSuffix(strings.TrimPrefix(u.Path, strings.TrimRight(b.Path, "/")+"/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return path, true
}
