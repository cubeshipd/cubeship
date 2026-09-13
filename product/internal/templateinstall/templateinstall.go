// Package templateinstall installs templates from the catalog, updates
// them to newer releases and uninstalls them.
//
// To install, it reads the template's file at the commit the catalog
// recorded for a release, checks it with the same validator the catalog
// ran — the catalog's verdict is a listing, not something this instance
// takes on trust — and creates what the file declares: a project and
// environment when they do not exist yet, databases, object stores and
// apps, with their domains, attachments and variables, and then deploys
// the apps. An update compares a newer release with the one installed and
// applies what changed without deleting anything; an uninstall deletes the
// apps, and the data only when asked to.
//
// Every change runs detached from the request, recorded as it goes, and
// a failure puts back exactly what that run changed.
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

// Statuses an installation can be in. A failed one never finished
// installing, and what it had created was deleted.
const (
	StatusInstalling  = "installing"
	StatusInstalled   = "installed"
	StatusFailed      = "failed"
	StatusUninstalled = "uninstalled"
)

// What a run does, and how it went.
const (
	RunInstall   = "install"
	RunUpdate    = "update"
	RunUninstall = "uninstall"

	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

// Kinds of resource a run creates.
const (
	KindProject     = "project"
	KindEnvironment = "environment"
	KindDatabase    = "database"
	KindStore       = "store"
	KindApp         = "app"
	// A domain or an attachment is only ever a resource of an update:
	// an install's go with the app it deletes, an update's are added to
	// apps that stay.
	KindDomain     = "domain"
	KindAttachment = "attachment"
)

// Resource is one thing a run created.
type Resource struct {
	Kind string `json:"kind"`
	// Key is the template's key for it. Empty for a project or an
	// environment, which the template does not name.
	Key string `json:"key,omitempty"`
	// Name is a project's slug, "project/environment" for an environment,
	// a database's or a store's name, an app's full reference, "reference
	// host" for a domain, and "reference database name" or "reference
	// store name bucket" for an attachment.
	Name string `json:"name"`
}

// Install is a template installed on this instance.
type Install struct {
	ID          int64
	Owner       string
	Repo        string
	Release     string
	Commit      string
	Project     string
	Environment string
	Status      string
	// Manifest is the template at the release installed, which an update
	// compares the next one against.
	Manifest *template.Normalized
	// Answers are the inputs' answers, secrets left out: those exist only
	// in the variables of the apps that use them.
	Answers map[string]string
	// Resources is everything the installation owns, oldest first.
	Resources []Resource
	CreatedBy int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// resource is what a template key names on the instance, of one kind.
func (in *Install) resource(kind, key string) (string, bool) {
	for _, r := range in.Resources {
		if r.Kind == kind && r.Key == key {
			return r.Name, true
		}
	}
	return "", false
}

// Run is one install, update or uninstall of an installation.
type Run struct {
	ID          int64
	InstallID   int64
	Kind        string
	FromRelease string
	ToRelease   string
	Status      string
	// Step is what the run is doing now, in a sentence. Empty once done.
	Step  string
	Error string
	// Created is what this run made, oldest first — what a failure
	// deletes, newest first.
	Created []Resource
	// Snapshot is each existing app as an update found it.
	Snapshot []AppSnapshot
	// KeepData is an uninstall leaving databases and stores in place.
	KeepData   bool
	CreatedBy  int64
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// AppSnapshot is an app before an update changed it: enough to put back
// what the update touched, and nothing it did not.
type AppSnapshot struct {
	Ref             string  `json:"ref"`
	SourceChanged   bool    `json:"source_changed"`
	SettingsChanged bool    `json:"settings_changed"`
	Source          string  `json:"source"`
	Image           string  `json:"image"`
	Tag             string  `json:"tag"`
	Repo            string  `json:"repo"`
	GitRef          string  `json:"git_ref"`
	Dockerfile      string  `json:"dockerfile"`
	Health          string  `json:"health"`
	CPU             float64 `json:"cpu"`
	Memory          int64   `json:"memory_bytes"`
	AutoscaleMin    int     `json:"autoscale_min"`
	AutoscaleMax    int     `json:"autoscale_max"`
	AutoscaleCPU    float64 `json:"autoscale_cpu"`
	Scale           int     `json:"scale"`
	Spread          bool    `json:"spread"`
	// Env is each variable the update set, as it was before: null for
	// one that did not exist.
	Env map[string]*string `json:"env"`
}

var (
	ErrNotFound = errors.New("template installation not found")
	// ErrCatalogUnavailable is the catalog, or GitHub behind it, not
	// answering — including on an instance that was given no catalog.
	ErrCatalogUnavailable = errors.New("the template catalog could not be reached")
	ErrTemplateNotFound   = errors.New("no template is listed at that address")
	ErrReleaseNotFound    = errors.New("the catalog accepted no release of this template by that tag")
	ErrTooNew             = errors.New("this template needs a newer Cubeship than this instance runs")
	ErrBusy               = errors.New("this installation is already being changed; wait for that to finish")
	ErrNotInstalled       = errors.New("this installation is not installed")
	ErrUpToDate           = errors.New("this installation is already on that release")
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
// project, environment or a name — that a run cannot use.
type InputError struct {
	Key     string
	Message string
}

func (e *InputError) Error() string { return fmt.Sprintf("%s: %s", e.Key, e.Message) }

// TakenError is something a run would create that already exists.
type TakenError struct {
	Kind string
	Name string
}

func (e *TakenError) Error() string {
	return fmt.Sprintf("a %s called %s already exists", e.Kind, e.Name)
}
