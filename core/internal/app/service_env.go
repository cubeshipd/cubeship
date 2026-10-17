package app

import (
	"context"
	"cubeship/internal/envvar"
	"cubeship/internal/user"
)

// Env returns the app's own variables plus the effective environment its
// container actually runs with: the project's, overridden by the
// environment's, overridden by the app's, each value labelled with the
// level that won it.
//
// Without this there is no way to see what an app is configured with —
// which is what made replacing the whole map so easy to do by accident.
func (s *Service) Env(ctx context.Context, caller *user.User, ref Reference) (envvar.Map, []envvar.Resolved, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, nil, err
	}
	if err := user.AllowSecrets(caller, user.ResApps, ref.Project); err != nil {
		return nil, nil, err
	}
	p, err := s.projects.Repo().ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.projects.EnvironmentRepo().ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, nil, err
	}
	// The same layers the container is built from, in the same order —
	// this is the screen that answers "where did DATABASE_URL come
	// from", and it can only answer it if it is looking at what the
	// deploy looks at.
	var stores envvar.Map
	if s.datastores != nil {
		if stores, err = s.datastores.VarsForApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	var buckets envvar.Map
	if s.objectStores != nil {
		if buckets, err = s.objectStores.VarsForApp(ctx, a.ID); err != nil {
			return nil, nil, err
		}
	}
	resolved := envvar.Resolve(
		envvar.Layer{Source: envvar.SourceProject, Vars: p.Env},
		envvar.Layer{Source: envvar.SourceEnvironment, Vars: e.Env},
		envvar.Layer{Source: envvar.SourceDatastore, Vars: stores},
		envvar.Layer{Source: envvar.SourceObjectStore, Vars: buckets},
		envvar.Layer{Source: envvar.SourceApp, Vars: a.Env})
	return a.Env, resolved, nil
}

// requireEnvRole is the role writing an app's environment takes.
//
// For an app that runs a published image it is a member's: the variables
// are the container's, read by whatever that image already does with
// them.
//
// For an app that builds, they are also *build input*. Railpack reads
// the environment to work out how to build the repository, and turns
// RAILPACK_INSTALL_CMD, RAILPACK_BUILD_CMD and RAILPACK_START_CMD into
// the commands the build runs — inside the privileged builder, on this
// host. Writing them is therefore the same act as building, and it takes
// the same role: an app's own variables win the merge over its
// environment's and its project's, so a member who could write them
// could decide what an admin's app builds and runs.
func (s *Service) requireEnvRole(caller *user.User, a *Scoped) error {
	return requireSource(caller, Source(a.Source))
}

// SetEnv replaces the app's own variables, deleting any key not present.
// They are layered on top of (and override) its environment's and
// project's.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, ref Reference, env envvar.Map) (*Scoped, error) {
	// Resolved as a member first, so someone who cannot write this app's
	// environment still gets the answer an unknown app gets rather than
	// two different refusals. See requireEnvRole for the check after.
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().SetEnv(ctx, a.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, ref Reference, set envvar.Map, unset []string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelManage)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().MergeEnv(ctx, a.ID, set, unset)
}
