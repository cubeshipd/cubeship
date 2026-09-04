// Package github is what lets Cubeship act as a GitHub App: clone a
// private repository, and deploy when someone pushes to one.
//
// One App per instance, registered by whoever runs the VPS. An
// organization then installs it on its own GitHub account, and that
// installation is what grants access — which is why installations belong
// to an organization here. Without that, one tenant could deploy
// another's private code just by naming its URL.
package github

import (
	"errors"
	"strings"
	"time"
)

// Installation is one GitHub App installation, held by a Cubeship
// organization.
type Installation struct {
	ID        int64
	OrgID     int64
	GitHubID  int64
	Account   string
	CreatedAt time.Time
}

var (
	ErrNotConfigured   = errors.New("this instance is not registered as a GitHub App")
	ErrNoInstallation  = errors.New("this organization has not installed the GitHub App on that account")
	ErrNotFound        = errors.New("no such installation")
	ErrBadSignature    = errors.New("the webhook signature does not match")
	ErrNoWebhookSecret = errors.New("no webhook secret is configured, so a webhook cannot be trusted")
)

// Repository is a GitHub repository named the way both a URL and a
// webhook payload can be reduced to: the account it belongs to and its
// name.
type Repository struct {
	Owner string
	Name  string
}

// FullName is "owner/name", which is how GitHub itself names it.
func (r Repository) FullName() string { return r.Owner + "/" + r.Name }

// ParseRepositoryURL reduces a clone URL to the repository it names, and
// reports whether it is on GitHub at all. A repository elsewhere is not
// an error — it just cannot use any of this.
//
// It accepts what someone would paste: with or without .git, with or
// without a trailing slash, http or https, and the browser URL as
// readily as the clone one.
func ParseRepositoryURL(url string) (Repository, bool) {
	rest, ok := strings.CutPrefix(url, "https://")
	if !ok {
		if rest, ok = strings.CutPrefix(url, "http://"); !ok {
			return Repository{}, false
		}
	}
	host, path, found := strings.Cut(rest, "/")
	if !found {
		return Repository{}, false
	}
	// A URL can carry credentials; the host is what follows them.
	if _, after, ok := strings.Cut(host, "@"); ok {
		host = after
	}
	if !strings.EqualFold(host, "github.com") && !strings.EqualFold(host, "www.github.com") {
		return Repository{}, false
	}

	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	owner, name, found := strings.Cut(path, "/")
	if !found || owner == "" || name == "" {
		return Repository{}, false
	}
	// Anything deeper is a page in the repository, not the repository.
	if strings.Contains(name, "/") {
		name, _, _ = strings.Cut(name, "/")
	}
	return Repository{Owner: owner, Name: name}, true
}

// BranchOf reduces a push event's ref to the branch it names, and
// reports whether it was a branch at all. A tag or any other ref is not
// a branch, and an app tracking a branch must not deploy on one.
func BranchOf(ref string) (string, bool) {
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	return branch, ok && branch != ""
}
