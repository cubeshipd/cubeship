// Package discovery keeps the template catalog: it finds the GitHub
// repositories carrying the cubeship-template topic, reads each new
// release at the commit its tag points to, validates it, and records it
// as accepted or rejected.
//
// It runs on a timer, not on requests. cubeship.dev only reads what this
// writes.
package discovery

import (
	"context"
	"errors"
	"time"

	"cubeship/template"
)

// Topic is what marks a repository as a template.
const Topic = "cubeship-template"

// The files a release is read from, at the root of its commit.
const (
	TemplateFile = "template.yaml"
	ReadmeFile   = "README.md"
	IconFile     = "icon.png"
)

// Size ceilings, per file. A template is a page of YAML; anything near
// these is not one.
const (
	templateLimit = 256 << 10
	readmeLimit   = 512 << 10
	iconLimit     = 512 << 10
)

// Why a repository is not listed.
const (
	HiddenBlocked  = "blocked"
	HiddenGone     = "gone"     // deleted, private, or unreachable
	HiddenUntagged = "untagged" // the topic was removed
)

// Repo is a repository as GitHub describes it.
type Repo struct {
	NodeID      string
	ID          int64
	Owner       string
	Name        string
	OwnerAvatar string
	Description string
	URL         string
	Stars       int
	Private     bool
	Topics      []string
	Releases    []Release // newest first
}

// Release is one of a repository's releases.
type Release struct {
	Tag         string
	Name        string
	URL         string
	Commit      string // the commit the tag points to; empty when GitHub cannot say
	PublishedAt time.Time
	Draft       bool
	Prerelease  bool
}

// Known is a repository already in the catalog.
type Known struct {
	ID     int64
	NodeID string
}

// Indexed is a release once read: what the catalog stores.
type Indexed struct {
	RepositoryID int64
	Tag          string
	Commit       string
	Name         string
	URL          string
	PublishedAt  time.Time
	Accepted     bool
	Problems     []template.Diagnostic
	Manifest     *template.Normalized
	Source       string
	Readme       string
	IconKey      string
}

var (
	// ErrNotFound is a file the commit does not have.
	ErrNotFound = errors.New("not found")
	// ErrTooLarge is a file over its ceiling.
	ErrTooLarge = errors.New("too large")
)

// GitHub is what the catalog reads.
type GitHub interface {
	// Search is every public repository with the topic.
	Search(ctx context.Context, topic string) ([]Repo, error)
	// Lookup reads repositories by node id. One missing from the result
	// no longer exists, or is no longer visible.
	Lookup(ctx context.Context, nodeIDs []string) (map[string]Repo, error)
	// File is one file at one commit: ErrNotFound, ErrTooLarge, or
	// another error for anything worth trying again.
	File(ctx context.Context, owner, name, commit, path string, limit int64) ([]byte, error)
}

// Store is where the catalog is kept.
type Store interface {
	Blocked(ctx context.Context) (map[string]bool, error)
	Repositories(ctx context.Context) ([]Known, error)
	// SaveRepository writes what GitHub says about r; hidden is "" to list it.
	SaveRepository(ctx context.Context, r Repo, hidden string) error
	Hide(ctx context.Context, id int64, reason string) error
	Indexed(ctx context.Context, repositoryID int64, tag, commit string) (bool, error)
	SaveRelease(ctx context.Context, r Indexed) error
}

// Objects is the bucket icons go to.
type Objects interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
}
