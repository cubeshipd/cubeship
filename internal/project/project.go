// Package project owns projects: the grouping this instance's apps are
// organized into, and the outermost level environment variables can be
// set at.
//
// A project always contains at least one environment; creating one
// creates that environment too. See internal/environment.
package project

import (
	"context"
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
	ID   int64
	Slug string
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

	// ErrAlreadyExists reports a slug already used on this instance.
	ErrAlreadyExists = errors.New("project already exists")

	// ErrNoTeardown reports that the app module was never wired in, so a
	// delete cannot know what it would leave running. Refusing is the
	// only safe answer: rows would go and containers would stay.
	ErrNoTeardown = errors.New("cannot delete: the app module is not wired in")
)

// AppTeardown removes the apps under something being deleted — their
// containers first, then their rows.
//
// Deleting a project or an environment takes everything under it with
// it, and stopping a container is the app module's job. Dependencies run
// one way, so project knows nothing of app: the app service is handed in
// at wiring time.
//
// Authorization has already happened by the time one of these is called
// — the caller had to be able to delete the thing above — so neither
// takes a caller.
type AppTeardown interface {
	DeleteAppsInProject(ctx context.Context, projectID int64) error
	DeleteAppsInEnvironment(ctx context.Context, environmentID int64) error
}
