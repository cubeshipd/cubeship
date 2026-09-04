// Package app owns apps and their lifecycle: the App and Deployment
// entities, their persistence, the zero-downtime deploy orchestrator, and
// the HTTP and MCP surfaces both are reached through.
//
// An app lives in an environment, inside a project, inside an
// organization — and the organization is what authorizes every action on
// it.
package app

import (
	"errors"
	"time"

	"cubeship/internal/envvar"
)

// App is one deployable service: a name, the domain Traefik routes to
// it, and whichever container is currently serving it.
//
// Where its image comes from is not stored here. For an app pushed to the
// embedded registry the path is derived from its reference, so an app
// created before the instance had a domain gets a correct push path the
// moment one is configured.
type App struct {
	ID            int64
	OrgID         int64
	ProjectID     int64
	EnvironmentID int64
	Name          string
	Domain        string
	ContainerID   string
	Status        string
	Env           envvar.Map
	CreatedAt     time.Time
}

// Deployment is one attempt to run a new image for an app. It is created
// when the deploy is accepted and finished when it ends, so it is also
// how a caller finds out how a deploy went after the request that
// started it is long gone.
type Deployment struct {
	ID        int64
	AppID     int64
	ImageRef  string
	Status    string
	Error     string
	CreatedAt time.Time
}

// Deployment statuses.
const (
	DeploymentPending   = "pending"
	DeploymentSucceeded = "succeeded"
	DeploymentFailed    = "failed"
)

// Done reports whether the deployment has finished, either way.
func (d *Deployment) Done() bool {
	return d.Status == DeploymentSucceeded || d.Status == DeploymentFailed
}

// Statuses an app can be in. "pending" is the initial state of an app
// that has never had an image pushed to it.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDown    = "down"
)

// Port is the port every app container is expected to listen on. A
// per-app configurable port is not supported.
const Port = 8080

// Network is the Docker network app containers must join. Traefik
// resolves a container's backend IP on this network specifically (see the
// traefik.docker.network label), so a container left on the default
// bridge is invisible to the proxy and its domain serves 503.
const Network = "cubeship"

var (
	// ErrNotFound covers both "no such app" and "not yours to see", so a
	// response never confirms that another organization's app exists.
	ErrNotFound = errors.New("app not found")

	// ErrAlreadyExists reports a name already taken in that environment.
	ErrAlreadyExists = errors.New("app already exists")

	// ErrNoContainer reports an app that has never had an image pushed
	// to it, so there is nothing to read logs from.
	ErrNoContainer = errors.New("app has no running container")

	// ErrDeploymentNotFound covers a deployment id that does not belong
	// to the app it was asked for.
	ErrDeploymentNotFound = errors.New("deployment not found")

	// ErrNoRegistry reports that the instance has no domain yet, so
	// there is no registry to push to or pull from.
	ErrNoRegistry = errors.New("no registry: set a domain in the instance settings first")
)
