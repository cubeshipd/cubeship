// Package datastore owns the databases Cubeship runs for the apps on it:
// the Datastore entity, its persistence, the container lifecycle behind
// it, and the HTTP and MCP surfaces both are reached through.
//
// A datastore lives in an environment, inside a project — the same
// address an app has, because it is the same kind of thing to reach for.
// An app in `acme/api/production` and the database it talks to belong to
// one deployment of one project, and `staging` holding different data
// from `production` is what an environment already means.
//
// It is not an app, which is why it is not in that package: there is no
// image to push, no source to build, no domain, no zero-downtime swap. A
// database is provisioned once and then runs.
package datastore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"cubeship/internal/slug"
)

// Datastore is one database server, running in a container of its own on
// the shared network.
type Datastore struct {
	ID            int64
	ProjectID     int64
	EnvironmentID int64
	// Slug names it within its environment and is permanent. It is a
	// component of the container name its apps resolve, so renaming it
	// would break every connection already handed out.
	Slug string
	// Description is what this database is for, in a sentence.
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
}

// Statuses a datastore can be in.
const (
	// StatusProvisioning is the state it is created in: the row exists
	// and the container is being pulled and started, which happens
	// detached from the request that asked for it.
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusDown         = "down"
	// StatusFailed is provisioning that did not finish. Error says why.
	StatusFailed = "failed"
)

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

// Network is the Docker network these containers join — the same one
// apps are on, which is the whole of how an app reaches one.
const Network = "cubeship"

// containerPrefix is what a datastore's container is named under.
//
// Deliberately not the prefix an app's container takes. Two namespaces
// that cannot collide are two namespaces nobody has to keep apart: an
// app and a database in one environment may both be called `api`
// without either being unable to start, and the name says which is
// which when somebody is reading `docker ps` at three in the morning.
const containerPrefix = "cubeship-db-"

var (
	// ErrNotFound covers both "no such datastore" and a malformed
	// reference resolved against a real instance.
	ErrNotFound = errors.New("datastore not found")

	// ErrAlreadyExists reports a slug already used in that environment.
	ErrAlreadyExists = errors.New("a datastore with that slug already exists in this environment")

	// ErrUnknownEngine is an engine this version cannot run. Refused at
	// creation: accepting one would be a datastore that can never start.
	ErrUnknownEngine = errors.New("unknown database engine")

	// ErrUnknownVersion is a version this version of Cubeship does not
	// offer for that engine.
	ErrUnknownVersion = errors.New("unknown version for this engine")

	// ErrImmutable is an attempt to change what cannot change after
	// creation — see Service.Update for why each one cannot.
	ErrImmutable = errors.New("a datastore's slug, engine and version cannot be changed after it is created")

	// ErrBadUsername is a login the engine itself will not create.
	ErrBadUsername = errors.New("invalid username")

	// ErrBadPassword is an empty password. Nothing else is refused: the
	// password goes into a URL through proper encoding, so no character
	// in it is a problem.
	ErrBadPassword = errors.New("password must not be empty")

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

	// ErrDifferentEnvironment refuses attaching an app to a database in
	// another environment. The network would carry it, but it would
	// make `staging` write to production data through a link nothing on
	// either screen shows.
	ErrDifferentEnvironment = errors.New("an app can only be attached to a datastore in its own environment")

	// ErrBadPort is a host port outside what may be published.
	ErrBadPort = errors.New("port must be between 1024 and 65535")

	// ErrPortTaken is a host port another datastore already answers on.
	ErrPortTaken = errors.New("another datastore is already exposed on that port")

	// ErrNoPortsLeft reports the automatic range exhausted.
	ErrNoPortsLeft = fmt.Errorf("no free port left in the range %d-%d; name one explicitly", PortRangeStart, PortRangeEnd)

	// ErrNotProvisioned is an operation that needs a container behind
	// it on a datastore that has none yet.
	ErrNotProvisioned = errors.New("this datastore has no running container yet")
)

// Reference names one datastore: <project>/<environment>/<slug>.
//
// The same shape an app's reference has, because it addresses the same
// place. A bare slug identifies nothing — it is unique only within its
// environment.
type Reference struct {
	Project     string
	Environment string
	Name        string
}

func (r Reference) String() string {
	return strings.Join([]string{r.Project, r.Environment, r.Name}, "/")
}

// ContainerName is what this datastore's container is called, and so the
// host its apps connect to: Docker resolves a container's name on the
// shared network, and that is the whole of the internal wiring.
func (r Reference) ContainerName() string {
	return containerPrefix + strings.Join(
		[]string{r.Project, r.Environment, r.Name}, "-")
}

// ParseReference reads "project/environment/name", accepting two parts
// as shorthand for the production environment. Every part is validated
// as a slug, so a malformed reference can never reach a container name.
func ParseReference(s string) (Reference, error) {
	parts := strings.Split(strings.Trim(s, "/"), "/")
	var ref Reference
	switch len(parts) {
	case 2:
		ref = Reference{Project: parts[0], Environment: "production", Name: parts[1]}
	case 3:
		ref = Reference{Project: parts[0], Environment: parts[1], Name: parts[2]}
	default:
		return Reference{}, fmt.Errorf("%q is not a datastore reference: want project/environment/name", s)
	}
	for _, part := range []string{ref.Project, ref.Environment, ref.Name} {
		if !slug.Valid(part) {
			return Reference{}, fmt.Errorf("%q is not a datastore reference: %q %w", s, part, slug.ErrInvalid)
		}
	}
	return ref, nil
}

// Scoped is a datastore with the slugs it lives under, which is what
// every surface reports and what a reference is built from.
type Scoped struct {
	Datastore
	ProjectSlug     string
	EnvironmentSlug string
	// Attachments are the apps that receive this datastore's connection
	// variables. Loaded with the datastore rather than asked for
	// separately: what a database is wired to is most of what anyone
	// wants to know about it.
	Attachments []Attachment
}

// ReferenceOf builds the reference identifying d.
func ReferenceOf(d *Scoped) Reference {
	return Reference{Project: d.ProjectSlug, Environment: d.EnvironmentSlug, Name: d.Slug}
}

// Attachment is one app wired to one datastore.
type Attachment struct {
	ID          int64
	DatastoreID int64
	AppID       int64
	// AppSlug is the app's name within the environment, joined on read
	// so nothing has to reach into the app module to render one.
	AppSlug string
	// Prefix is what the injected variables are named under. Empty for
	// the usual case — see envvar names in engine.go.
	Prefix    string
	CreatedAt time.Time
}
