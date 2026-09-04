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
type Project struct {
	ID    int64
	OrgID int64
	Slug  string
	Name  string
	// Description is what the project is for, in a sentence or two. The
	// slug cannot say it — it is a path component — and the name barely
	// can.
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

	// ErrNameRequired refuses an update that would leave a project with
	// nothing to call it. Clearing the description is fine; clearing the
	// name is not.
	ErrNameRequired = errors.New("name cannot be empty")
)
