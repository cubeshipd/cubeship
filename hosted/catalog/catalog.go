// Package catalog reads cubeship-templates and serves its validated directories.
package catalog

import (
	"context"
	"cubeship/template"
	"time"
)

// Topic remains only to read metadata stored before the monorepo cutover.
const Topic = "cubeship-template"
const (
	TemplateFile  = "template.yaml"
	ReadmeFile    = "README.md"
	IconFile      = "icon.png"
	templateLimit = 256 << 10
	readmeLimit   = 512 << 10
	iconLimit     = 512 << 10
	HiddenGone    = "gone"
)

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
}
type Known struct {
	ID     int64
	NodeID string
}
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
	Icon         []byte
}
type Report struct {
	Repositories int `json:"repositories"`
	Accepted     int `json:"accepted"`
	Rejected     int `json:"rejected"`
	Hidden       int `json:"hidden"`
	Failed       int `json:"failed"`
}

// SnapshotEntry is one validated directory at a single source commit.
type SnapshotEntry struct {
	Repository Repo
	Indexed    Indexed
}

type Store interface {
	Publish(context.Context, []SnapshotEntry) (accepted, hidden int, err error)
}
