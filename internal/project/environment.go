package project

import (
	"errors"
	"time"

	"cubeship/internal/envvar"
)

// Environment lives inside a Project and holds the apps that share a
// stage of its lifecycle — production, staging, a preview.
//
// It is modelled in this package rather than one of its own because it
// has no existence apart from its project: a project is always created
// with an environment, an environment cannot be moved between projects,
// and deleting the last one is forbidden. Splitting them would mean two
// packages that can only ever be used together, plus an import cycle to
// work around.
// An environment has no display name either. Its slug is its name, for
// the same reason a project's and an app's are.
type Environment struct {
	ID        int64
	ProjectID int64
	Slug      string
	// Description is what this stage of the project's lifecycle is for.
	// The slug cannot say it — it is a path component — so this is where
	// anything beyond the name goes.
	Description string
	Env         envvar.Map
	CreatedAt   time.Time
}

// ProductionEnvSlug is the environment every project is created with. It
// can never be deleted, so apps and deploys can always assume at least
// one environment exists per project.
const ProductionEnvSlug = "production"

var (
	// ErrEnvironmentNotFound covers both "no such environment" and "not
	// yours to see".
	ErrEnvironmentNotFound = errors.New("environment not found")

	// ErrEnvironmentExists reports a slug already used in this project.
	ErrEnvironmentExists = errors.New("environment already exists")

	// ErrProductionUndeletable guards the one environment every project
	// must keep.
	ErrProductionUndeletable = errors.New(`the "production" environment can never be deleted`)

	// ErrEnvironmentHasApps refuses a delete that would orphan apps.
	ErrEnvironmentHasApps = errors.New("environment still has apps in it")
)
