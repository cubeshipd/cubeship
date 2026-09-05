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

	// ErrUsernameRequired and ErrPasswordRequired are the two fields
	// with nothing sensible to default.
	ErrUsernameRequired = errors.New("username is required")
	ErrPasswordRequired = errors.New("password is required")

	// ErrBadToken is a claim without the setup token this instance
	// generated. See Token for what it is and where to read it.
	ErrBadToken = errors.New("wrong setup token; it is printed by the installer and stored in the data directory as " + TokenFileName)
)

// Status is what a browser asks before showing anything: is there an
// account to sign in to, or is this instance still unclaimed?
type Status struct {
	Needed bool `json:"needed"`
	// TokenRequired says the claim has to carry the setup token, so a
	// browser knows to ask for it rather than failing at submit.
	TokenRequired bool `json:"token_required"`
}
