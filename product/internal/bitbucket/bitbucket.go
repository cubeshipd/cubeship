// Package bitbucket connects Bitbucket Cloud accounts to this instance.
package bitbucket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Repository struct{ Workspace, Slug string }

func (r Repository) FullName() string { return r.Workspace + "/" + r.Slug }

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*$`)

func ParseRepositoryURL(raw string) (Repository, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "bitbucket.org") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return Repository{}, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git"), "/")
	if len(parts) < 3 || parts[0] != "" || !repositoryPart.MatchString(parts[1]) || !repositoryPart.MatchString(parts[2]) {
		return Repository{}, false
	}
	return Repository{parts[1], parts[2]}, true
}
func BranchOf(ref string) (string, bool) {
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	return branch, ok && branch != ""
}

var (
	ErrBadSignature  = errors.New("the webhook signature does not match")
	ErrNotConfigured = errors.New("configure the Bitbucket OAuth consumer first")
	ErrState         = errors.New("this OAuth state is expired, already used, or belongs to another user")
	ErrReconnect     = errors.New("reconnect the Bitbucket account")
	ErrRepository    = errors.New("name an accessible Bitbucket repository as workspace/repository")
	ErrProvider      = errors.New("Bitbucket request failed; retry or check the consumer permissions")
)

func VerifyWebhook(body []byte, secret, signature string) error {
	if secret == "" {
		return ErrBadSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(want)) {
		return ErrBadSignature
	}
	return nil
}

type Connection struct {
	ID           int64     `json:"id"`
	Account      string    `json:"account"`
	UUID         string    `json:"uuid"`
	UserID       int64     `json:"user_id"`
	CreatedAt    time.Time `json:"created_at"`
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	consumerID   string
}
type RepositoryRef struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}
type Branch struct {
	Name string `json:"name"`
}
