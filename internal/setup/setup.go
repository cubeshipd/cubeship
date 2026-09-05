// Package setup owns the one thing that happens before anything else can:
// claiming a fresh instance.
//
// Cubeship is installed with one command and reached at its IP. There is
// no user, no organization, and no way to sign in until someone opens it
// and creates the first account — which is also the only moment an
// account can be created without already having one.
package setup

import "errors"

var (
	// ErrAlreadySetUp reports that the instance has been claimed. Setup
	// is not a way to add users — that is an organization admin's job,
	// and it requires already being one.
	ErrAlreadySetUp = errors.New("this instance has already been set up")

	// ErrUsernameRequired, ErrPasswordRequired and ErrOrgRequired are
	// the three fields with nothing sensible to default.
	//
	// The organization is asked for rather than invented. A slug is
	// permanent — it is the first component of every app's registry
	// reference — so an organization named on someone's behalf is one
	// they are stuck with, and "my-organization" was never anybody's
	// answer.
	ErrUsernameRequired = errors.New("username is required")
	ErrPasswordRequired = errors.New("password is required")
	ErrOrgRequired      = errors.New("organization is required")
)

// Status is what a browser asks before showing anything: is there an
// account to sign in to, or is this instance still unclaimed?
type Status struct {
	Needed bool `json:"needed"`
}
