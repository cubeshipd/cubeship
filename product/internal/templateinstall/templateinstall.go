// Package templateinstall installs a template from the catalog.
//
// It reads the template's file at the commit the catalog recorded for a
// release, checks it with the same validator the catalog ran — the
// catalog's verdict is a listing, not something this instance takes on
// trust — and creates what the file declares: a project and environment
// when they do not exist yet, databases, object stores and apps, with
// their domains, attachments and variables, and then deploys the apps.
//
// The work runs detached from the request, recorded as it goes. Anything
// that fails undoes exactly what the install created, newest first, and
// nothing that existed before it ran.
//
// It sits above project, app, datastore and objectstore, and knows them
// through the narrow interfaces in service.go; server is the only package
// that knows it exists.
package templateinstall

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cubeship/template"
)

// Statuses an install can be in. A failed install has already been undone.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Kinds of resource an install creates, in the order it creates them.
const (
	KindProject     = "project"
	KindEnvironment = "environment"
	KindDatabase    = "database"
	KindStore       = "store"
	KindApp         = "app"
)

// Resource is one thing an install created. Name is a project's slug,
// "project/environment" for an environment, a database's or a store's
// name, and an app's full reference.
type Resource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Install is one install of a template and how it went.
type Install struct {
	ID          int64
	Owner       string
	Repo        string
	Release     string
	Commit      string
	Project     string
	Environment string
	Status      string
	// Step is what the install is doing now, in a sentence. Empty once
	// it has finished.
	Step  string
	Error string
	// Resources is what it created, oldest first — which is also what a
	// failure deletes, newest first.
	Resources  []Resource
	CreatedBy  int64
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Done reports whether the install has finished, either way.
func (i *Install) Done() bool { return i.Status != StatusRunning }

var (
	ErrNotFound = errors.New("template install not found")
	// ErrCatalogUnavailable is the catalog, or GitHub behind it, not
	// answering — including on an instance that was given no catalog.
	ErrCatalogUnavailable = errors.New("the template catalog could not be reached")
	ErrTemplateNotFound   = errors.New("no template is listed at that address")
	ErrReleaseNotFound    = errors.New("the catalog accepted no release of this template by that tag")
	ErrTooNew             = errors.New("this template needs a newer Cubeship than this instance runs")
)

// InvalidTemplateError is a file the validator refused on this instance.
type InvalidTemplateError struct {
	Diagnostics []template.Diagnostic
}

func (e *InvalidTemplateError) Error() string {
	var messages []string
	for _, d := range e.Diagnostics {
		messages = append(messages, d.Message)
	}
	return "the template file is not valid: " + strings.Join(messages, "; ")
}

// InputError is an answer to one of the template's questions — or the
// project, environment or a name — that the install cannot use.
type InputError struct {
	Key     string
	Message string
}

func (e *InputError) Error() string { return fmt.Sprintf("%s: %s", e.Key, e.Message) }

// TakenError is something the install would create that already exists.
type TakenError struct {
	Kind string
	Name string
}

func (e *TakenError) Error() string {
	return fmt.Sprintf("a %s called %s already exists", e.Kind, e.Name)
}
