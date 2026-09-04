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

// App is one deployable service: a name, the domain Traefik routes to it,
// the registry image a push to it lands in, and whichever container is
// currently serving it.
type App struct {
	ID            int64
	OrgID         int64
	ProjectID     int64
	EnvironmentID int64
	Name          string
	Domain        string
	Image         string
	ContainerID   string
	Status        string
	Env           envvar.Map
	CreatedAt     time.Time
}

// Deployment is one attempt to run a new image for an app, successful or
// not. It is the audit trail behind an app's current state.
type Deployment struct {
	ID        int64
	AppID     int64
	ImageRef  string
	Status    string
	Error     string
	CreatedAt time.Time
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

	// ErrAlreadyExists reports a name already taken.
	//
	// App names are unique across the whole instance today, not per
	// environment — see the TODO on the apps table.
	ErrAlreadyExists = errors.New("app already exists")

	// ErrNoContainer reports an app that has never had an image pushed
	// to it, so there is nothing to read logs from.
	ErrNoContainer = errors.New("app has no running container")
)
