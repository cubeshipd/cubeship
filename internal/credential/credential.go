// Package credential holds the secrets this instance is wired to.
//
// It exists because one secret is one secret. An AWS access key is the
// same key whether Route 53 is writing a record with it, ECR is being
// pulled from with it, or a bucket is being read with it — and before
// this, each of those asked for it separately. Three copies of one
// credential is three places to rotate it and three chances to miss
// one.
//
// **A credential is a facilitator, not a kind of thing.** What is
// stored here is a label, an optional first half and a secret. It has
// no provider: most API tokens can only be read at the moment they are
// issued, so a secret that could only ever be used for the one job it
// was filed under would have to be issued again for the second.
//
// Which API is spoken with it belongs to the **use**, and every use
// keeps a row of its own: a registry names its provider and the
// credential it logs in with, a DNS provider names Route 53 or
// Cloudflare and the credential it writes through. One credential may
// be named by any number of them.
package credential

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Credential is one secret, and what it is called.
type Credential struct {
	ID int64

	// Label is what tells two credentials apart, and the only thing
	// here somebody chooses freely. It exists because "the AWS one"
	// stops identifying anything the moment there are two — a personal
	// account and a company's, staging and production.
	Label string

	// Username is the first half, and empty for a secret that is a
	// single value. What it means is the *use*'s business: an access
	// key id to one thing, a registry login to another.
	Username string

	// Password is the secret half. Stored as given and never returned:
	// whatever uses it takes the secret itself, so a hash could not be
	// sent to one, and an endpoint that handed it back would turn every
	// read of the list into a way out for it.
	Password string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Use is one thing that depends on a credential, for saying what breaks
// before it is deleted.
type Use struct {
	// What kind of thing it is — "registry", "DNS provider".
	Kind string
	// How a person recognises it.
	Name string
}

// String is how a use is named in the sentence that refuses a delete.
// Trimmed, because some uses are a kind with nothing to distinguish
// them — there is only one "instance's own DNS".
func (u Use) String() string { return strings.TrimSpace(u.Kind + " " + u.Name) }

var (
	// ErrNotFound covers both "no such credential" and an id from
	// another instance's URL.
	ErrNotFound = errors.New("credential not found")

	// ErrLabelTaken reports a label already in use. Labels are how a
	// person picks one out of a list, so two the same would make the
	// list unreadable rather than merely ambiguous.
	ErrLabelTaken = errors.New("a credential with that label already exists")

	// ErrLabelRequired is an empty label. There is nothing else to call
	// one by: the secret cannot be shown, and nothing else about a
	// credential is unique.
	ErrLabelRequired = errors.New("a label is required — it is how you tell two credentials apart")

	// ErrPasswordRequired is a missing secret. A credential with no
	// secret is a credential that reaches nothing.
	ErrPasswordRequired = errors.New("the secret is required")

	// ErrNoDependants reports that the modules using credentials were
	// never wired in, so a delete cannot know what it would break.
	// Refusing is the only safe answer.
	ErrNoDependants = errors.New("cannot delete: the modules that use credentials are not wired in")

	// ErrInUse refuses deleting a credential something still depends
	// on. The alternative is a registry that cannot authenticate and a
	// deploy that fails for a reason nobody can see from the screen
	// they were on.
	ErrInUse = errors.New("something is still using this credential")
)

// InUseError names what is still depending on it.
func InUseError(uses []Use) error {
	names := make([]string, 0, len(uses))
	for _, u := range uses {
		names = append(names, u.String())
	}
	return fmt.Errorf("%w: %s. Point those elsewhere first, or remove them",
		ErrInUse, strings.Join(names, ", "))
}
