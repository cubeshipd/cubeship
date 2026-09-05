// Package setup owns the one thing that happens before anything else can:
// claiming a fresh instance.
//
// Cubeship is installed with one command and reached at its IP. There is
// no user, no organization, and no way to sign in until someone opens it
// and creates the first account — which is also the only moment an
// account can be created without already having one.
package setup

import "errors"

// The first organization and project. They exist so the dashboard has
// somewhere to put an app immediately, and are replaced like any other —
// a slug never changes, so the way to a different name is a new one.
const (
	OrgSlug     = "my-organization"
	ProjectSlug = "default"
)

var (
	// ErrAlreadySetUp reports that the instance has been claimed. Setup
	// is not a way to add users — that is an organization admin's job,
	// and it requires already being one.
	ErrAlreadySetUp = errors.New("this instance has already been set up")

	// ErrUsernameRequired and ErrPasswordRequired are the two fields
	// with nothing sensible to default.
	ErrUsernameRequired = errors.New("username is required")
	ErrPasswordRequired = errors.New("password is required")
)

// Status is what a browser asks before showing anything: is there an
// account to sign in to, or is this instance still unclaimed?
type Status struct {
	Needed bool `json:"needed"`
}
