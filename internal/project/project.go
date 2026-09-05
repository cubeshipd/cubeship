// Package project owns projects: the grouping an organization's apps are
// organized into, and the outermost level environment variables can be
// set at.
//
// A project always contains at least one environment; creating one
// creates that environment too. See internal/environment.
package project

import (
	"errors"
	"time"

	"cubeship/internal/envvar"
)

// Project groups an organization's environments, and holds the variables
// every app in every one of them inherits.
// A project has no display name. Its slug is its name, the same rule an
// app has always followed: the slug is a path component of every
// registry reference underneath it, so it is the identifier that cannot
// change and the one everybody reads. A second, editable name was a
// second idea for one thing.
type Project struct {
	ID    int64
	OrgID int64
	Slug  string
	// Description is what the project is for, in a sentence or two. The
	// slug cannot say it — it is a path component — so this is where
	// anything beyond the name goes.
	Description string
	Env         envvar.Map
	CreatedAt   time.Time
}

var (
	// ErrNotFound covers both "no such project" and "not yours to see",
	// so probing a slug reveals nothing.
	ErrNotFound = errors.New("project not found")

	// ErrAlreadyExists reports a slug already used in this organization.
	ErrAlreadyExists = errors.New("project already exists")

	// ErrHasApps refuses a delete that would orphan apps.
	ErrHasApps = errors.New("project still has apps in it")
)
