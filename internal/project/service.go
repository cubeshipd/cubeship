package project

import (
	"context"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service holds the project and environment use cases. Authorization is
// the caller's own role — there is no tenant above a project — so every
// entry point starts at user.Require.
type Service struct {
	db *database.DB

	// apps is how deleting a project or an environment reaches the
	// containers running inside it. See AppTeardown.
	apps AppTeardown
	// datastores is the same for the databases in it. See
	// DatastoreTeardown.
	datastores DatastoreTeardown
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// SetAppTeardown wires the app module in. Called once, at startup, by
// the only package that knows every module exists.
func (s *Service) SetAppTeardown(t AppTeardown) { s.apps = t }

// SetDatastoreTeardown wires the datastore module in, the same way and
// at the same moment.
func (s *Service) SetDatastoreTeardown(t DatastoreTeardown) { s.datastores = t }

func (s *Service) Repo() *Repository                       { return NewRepository(s.db) }
func (s *Service) EnvironmentRepo() *EnvironmentRepository { return NewEnvironmentRepository(s.db) }

// Resolve looks up a project by slug and requires minRole of the caller.
//
// A caller who lacks the role gets ErrForbidden rather than ErrNotFound:
// they are signed in to this instance and can see its projects listed,
// so hiding one would only confuse them.
func (s *Service) Resolve(ctx context.Context, caller *user.User, projectSlug string, minRole user.Role) (*Project, error) {
	if err := user.Require(caller, minRole); err != nil {
		return nil, err
	}
	p, err := s.Repo().BySlug(ctx, projectSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	return p, nil
}

// ResolveEnvironment is Resolve one level down.
func (s *Service) ResolveEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string, minRole user.Role) (*Environment, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, minRole)
	if err != nil {
		return nil, err
	}
	e, err := s.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, ErrEnvironmentNotFound
	}
	return e, nil
}

// Create registers a project and its mandatory production environment
// atomically: a project with no environment has nowhere for an app to be
// created, and a production environment without its project would be
// unreachable through the API.
func (s *Service) Create(ctx context.Context, caller *user.User, projectSlug string) (*Project, *Environment, error) {
	if err := user.Require(caller, user.RoleAdmin); err != nil {
		return nil, nil, err
	}
	if slug.Reserved(projectSlug) {
		return nil, nil, slug.ErrReserved
	}
	if !slug.Valid(projectSlug) {
		return nil, nil, slug.ErrInvalid
	}
	var created *Project
	var env *Environment
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		var err error
		created, err = NewRepository(tx).Create(ctx, projectSlug)
		if err != nil {
			return err
		}
		env, err = NewEnvironmentRepository(tx).Create(ctx, created.ID, ProductionEnvSlug)
		return err
	})
	if database.IsUniqueViolation(err) {
		// The unique index decides, not a preceding lookup — see
		// database.IsUniqueViolation.
		return nil, nil, ErrAlreadyExists
	}
	if err != nil {
		return nil, nil, err
	}
	return created, env, nil
}

// Update changes a project's description. A nil field is left
// as it was.
//
// Not the slug. No slug in Cubeship is editable after the resource is
// created — project, environment or app — because every
// one of them is a path component of an app's registry reference, which
// is derived on read rather than stored. Renaming one would silently
// move every app under it: pushes configured against the old path would
// start failing and images already pushed would be stranded where
// nothing looks for them again. The identifier is the one promise the
// daemon makes to whatever is configured against it.
func (s *Service) Update(ctx context.Context, caller *user.User, projectSlug string, description *string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return s.Repo().Update(ctx, p.ID, description)
}

func (s *Service) List(ctx context.Context, caller *user.User) ([]*Project, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Env returns the variables set on a project. Reading at RoleMember:
// anyone who can deploy an app needs to see what it will inherit.
func (s *Service) Env(ctx context.Context, caller *user.User, projectSlug string) (envvar.Map, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return p.Env, nil
}

// SetEnv replaces the project's full set of variables, deleting any key
// not present. Every environment and every app in the project inherits
// what remains.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, projectSlug string, env envvar.Map) (*Project, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return p, s.Repo().SetEnv(ctx, p.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, projectSlug string, set envvar.Map, unset []string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return p, s.Repo().MergeEnv(ctx, p.ID, set, unset)
}

// Delete removes a project and everything in it: every app's container
// is stopped and removed, then the environments and the project itself.
//
// It does not refuse while apps remain. Deleting a project is a decision
// about the project, and making someone delete its apps one at a time to
// get there is bookkeeping, not a safeguard — the safeguard is the
// confirmation in front of it, which asks for the project's own name.
//
// Containers go first and outside the transaction, because Docker has no
// rollback. A failure there leaves the apps gone and the project
// standing, which a retry finishes; the reverse would leave a container
// running with nothing on the instance naming it.
func (s *Service) Delete(ctx context.Context, caller *user.User, projectSlug string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if s.apps == nil {
		return nil, ErrNoTeardown
	}
	if s.datastores == nil {
		return nil, ErrNoDatastoreTeardown
	}
	if err := s.apps.DeleteAppsInProject(ctx, p.ID); err != nil {
		return nil, err
	}
	// The apps go first, so nothing is left connecting to a database
	// while it is being taken away.
	if err := s.datastores.DeleteDatastoresInProject(ctx, p.ID); err != nil {
		return nil, err
	}
	return p, s.db.WithTx(ctx, func(tx database.Queryer) error {
		return NewRepository(tx).Delete(ctx, p.ID)
	})
}

// CreateEnvironment adds an environment to an existing project.
func (s *Service) CreateEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*Environment, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if slug.Reserved(envSlug) {
		return nil, slug.ErrReserved
	}
	if !slug.Valid(envSlug) {
		return nil, slug.ErrInvalid
	}
	env, err := s.EnvironmentRepo().Create(ctx, p.ID, envSlug)
	if database.IsUniqueViolation(err) {
		return nil, ErrEnvironmentExists
	}
	return env, err
}

func (s *Service) ListEnvironments(ctx context.Context, caller *user.User, projectSlug string) ([]*Environment, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.EnvironmentRepo().ListForProject(ctx, p.ID)
}

// EnvironmentEnv returns the variables set on one environment, plus the
// effective set an app there would inherit — the project's, overridden
// by this environment's.
func (s *Service) EnvironmentEnv(ctx context.Context, caller *user.User, projectSlug, envSlug string) (envvar.Map, []envvar.Resolved, error) {
	p, err := s.Resolve(ctx, caller, projectSlug, user.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, nil, ErrEnvironmentNotFound
	}
	resolved := envvar.Resolve(
		envvar.Layer{Source: envvar.SourceProject, Vars: p.Env},
		envvar.Layer{Source: envvar.SourceEnvironment, Vars: e.Env})
	return e.Env, resolved, nil
}

// SetEnvironmentEnv replaces one environment's full set of variables,
// deleting any key not present.
func (s *Service) SetEnvironmentEnv(ctx context.Context, caller *user.User, projectSlug, envSlug string, env envvar.Map) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, projectSlug, envSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().SetEnv(ctx, e.ID, env)
}

// MergeEnvironmentEnv adds or overwrites the given variables and removes
// the unset ones, leaving every other key untouched.
func (s *Service) MergeEnvironmentEnv(ctx context.Context, caller *user.User, projectSlug, envSlug string, set envvar.Map, unset []string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, projectSlug, envSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().MergeEnv(ctx, e.ID, set, unset)
}

// UpdateEnvironment changes an environment's description. Not
// its slug: unlike a project's, which the dashboard lets you rename
// with a warning, an environment's slug is the third component of every
// app reference under it and there is no equivalent screen for it yet.
// A nil field is left as it was.
func (s *Service) UpdateEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string, description *string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, projectSlug, envSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return s.EnvironmentRepo().Update(ctx, e.ID, description)
}

// DeleteEnvironment removes an environment and everything deployed in
// it. production is the one refusal left: it is created with the project
// and every app assumes it exists.
func (s *Service) DeleteEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, projectSlug, envSlug, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if e.Slug == ProductionEnvSlug {
		return nil, ErrProductionUndeletable
	}
	if s.apps == nil {
		return nil, ErrNoTeardown
	}
	if s.datastores == nil {
		return nil, ErrNoDatastoreTeardown
	}
	if err := s.apps.DeleteAppsInEnvironment(ctx, e.ID); err != nil {
		return nil, err
	}
	if err := s.datastores.DeleteDatastoresInEnvironment(ctx, e.ID); err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().Delete(ctx, e.ID)
}
