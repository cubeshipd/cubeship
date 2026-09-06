// Package datastore owns the databases Cubeship runs for the apps on
// it: the Datastore entity, its persistence, the container lifecycle
// behind it, and the HTTP and MCP surfaces both are reached through.
//
// A datastore belongs to the **instance**, not to a project. On one VPS
// the common shape is a single Postgres serving several small apps, and
// those apps are routinely in different projects — a database owned by
// one project could not be reached from another at all, not because
// anything prevented it but because the model had decided in advance
// that it was the wrong thing to want.
//
// What connects a database to anything is therefore an Attachment, and
// only that. An attachment names one app, gives it the connection
// string as environment variables, and may cross projects and
// environments freely.
//
// It is not an app, which is why it is not in that package: there is no
// image to push, no source to build, no domain, no zero-downtime swap. A
// database is provisioned once and then runs.
package datastore

import (
	"errors"
	"fmt"
	"time"
)

// Datastore is one database server, running in a container of its own
// on the shared network.
type Datastore struct {
	ID int64
	// Slug names it on the whole instance and is permanent. It is the
	// container's name, which is the host every attached app resolves,
	// so renaming it would break every connection already handed out.
	Slug string
	// Description is what this database is for, in a sentence. With no
	// project above it to say where it belongs, this is the only place
	// that can.
	Description string

	// Engine and Version say which server this runs. Neither is
	// editable — see Service.Update.
	Engine  Engine
	Version string

	// Username and Password are the login the engine is initialized
	// with, and the only one Cubeship knows about. Anything else is
	// created inside the database by whoever uses it.
	Username string
	Password string
	// Database is the one created on first start. Empty for an engine
	// with no such concept.
	Database string

	// ExposedPort is the host port this answers on from outside the
	// instance, or 0 for "reachable only by its neighbours" — the
	// default, and the right answer for almost every database.
	ExposedPort int

	ContainerID string
	Status      string
	// Error is why provisioning failed, when it did.
	Error     string
	CreatedAt time.Time

	// Attachments are the apps that receive this datastore's connection
	// variables. Loaded with the datastore rather than asked for
	// separately: with nothing above it, what a database is wired to is
	// the whole of where it sits in the instance.
	Attachments []Attachment
}

// Statuses a datastore can be in.
const (
	// StatusProvisioning is the state it is created in: the row exists
	// and the container is being pulled and started, which happens
	// detached from the request that asked for it.
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	// StatusStopped is a database somebody turned off. Distinct from
	// "down", which is a container that stopped on its own: one is a
	// decision and the other is a fault, and a screen that shows the
	// same word for both makes an operator go looking for a problem
	// they created on purpose.
	StatusStopped = "stopped"
	StatusDown    = "down"
	// StatusFailed is provisioning that did not finish. Error says why.
	StatusFailed = "failed"
)

// Network is the Docker network these containers join — the same one
// apps are on, which is the whole of how an app reaches one.
const Network = "cubeship"

// containerPrefix is what a datastore's container is named under.
//
// Deliberately not the prefix an app's container takes. Two namespaces
// that cannot collide are two namespaces nobody has to keep apart: a
// datastore called `api` and an app called `api` may both exist, and
// the name says which is which when somebody is reading `docker ps` at
// three in the morning.
const containerPrefix = "cubeship-db-"

// ContainerName is what slug's container is called, and so the host its
// apps connect to: Docker resolves a container's name on the shared
// network, and that is the whole of the internal wiring.
func ContainerName(slug string) string { return containerPrefix + slug }

// PortRangeStart and PortRangeEnd bound the host ports Cubeship hands
// out when someone exposes a datastore without naming one.
//
// High and out of the way on purpose. The daemon's own Postgres already
// publishes 5432 on loopback, and a bind on 0.0.0.0:5432 would collide
// with it — so the obvious number is the one number that cannot work.
// An explicit port is still allowed anywhere above 1024; the range is
// only what "just expose it" picks from.
const (
	PortRangeStart = 15000
	PortRangeEnd   = 15999
)

// reservedSlugs are names this module's own API needs as path segments
// beside a slug of the same shape.
//
// Only `engines`: a datastore is addressed at /datastores/{name}, and
// GET /datastores/engines lists what this release can run. Go's mux
// prefers the literal, so a datastore called `engines` would be a
// resource nothing could fetch. `settings` is refused one level up, by
// slug.Reserved, because the dashboard needs it at every level.
//
// Local rather than global: this collides with an address this module
// serves, and a word refused everywhere for one module's sake is a rule
// nobody can trace back to a reason.
var reservedSlugs = map[string]bool{"engines": true}

var (
	// ErrNotFound is "no such datastore".
	ErrNotFound = errors.New("datastore not found")

	// ErrAlreadyExists reports a slug already used on this instance.
	// Instance-wide now, not per environment: two databases cannot
	// share a name, because the name is the container.
	ErrAlreadyExists = errors.New("a datastore with that name already exists on this instance")

	// ErrReservedSlug is a name this module's own API answers at.
	ErrReservedSlug = errors.New(`"engines" is reserved: it is where the API lists the engines this release can run`)

	// ErrUnknownEngine is an engine this version cannot run. Refused at
	// creation: accepting one would be a datastore that can never start.
	ErrUnknownEngine = errors.New("unknown database engine")

	// ErrUnknownVersion is a version this version of Cubeship does not
	// offer for that engine.
	ErrUnknownVersion = errors.New("unknown version for this engine")

	// ErrBadUsername is a login the engine itself will not create.
	ErrBadUsername = errors.New("invalid username")

	// ErrBadPrefix is an attachment prefix that would not be a legal
	// environment variable name once a suffix is appended to it.
	ErrBadPrefix = errors.New(`prefix must be uppercase letters, digits and underscores, ending in "_" — e.g. "ANALYTICS_"`)

	// ErrPrefixTaken reports that the app already has a datastore
	// attached under that prefix. Two would be one variable with two
	// values, and which one won would be a detail of map iteration.
	ErrPrefixTaken = errors.New("that app already has a datastore attached under this prefix")

	// ErrAlreadyAttached reports the same pair attached twice.
	ErrAlreadyAttached = errors.New("that app is already attached to this datastore")

	// ErrNotAttached is detaching something that was never attached.
	ErrNotAttached = errors.New("that app is not attached to this datastore")

	// ErrBadPort is a host port outside what may be published.
	ErrBadPort = errors.New("port must be between 1024 and 65535")

	// ErrPortTaken is a host port another datastore already answers on.
	ErrPortTaken = errors.New("another datastore is already exposed on that port")

	// ErrNotRunning is an operation that needs a live container on a
	// database that has none.
	ErrNotRunning = errors.New("this database has no container running")

	// ErrNoPortsLeft reports the automatic range exhausted.
	ErrNoPortsLeft = fmt.Errorf("no free port left in the range %d-%d; name one explicitly", PortRangeStart, PortRangeEnd)
)

// Attachment is one app wired to one datastore.
//
// It carries the app's full reference rather than its bare name,
// because a datastore is no longer inside an environment: `api` alone
// identifies nothing here, and two apps called `api` in two projects
// may both be attached to the same database.
type Attachment struct {
	ID          int64
	DatastoreID int64
	AppID       int64
	// AppReference is project/environment/name, joined on read so
	// nothing has to reach into the app module to render or link one.
	AppReference string
	// Prefix is what the injected variables are named under. Empty for
	// the usual case — see the variable names in engine.go.
	Prefix    string
	CreatedAt time.Time
}
