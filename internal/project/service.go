package project

import (
	"context"
	"errors"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service holds the project and environment use cases. Authorization is
// always the owning organization's answer, so every entry point goes
// through org.Service.Resolve first.
type Service struct {
	db   *database.DB
	orgs *org.Service
}

func NewService(db *database.DB, orgs *org.Service) *Service {
	return &Service{db: db, orgs: orgs}
}

func (s *Service) Repo() *Repository                       { return NewRepository(s.db) }
func (s *Service) EnvironmentRepo() *EnvironmentRepository { return NewEnvironmentRepository(s.db) }

// Resolve looks up a project by slug within an organization the caller is
// authorized in at minRole. An unknown organization, an organization the
// caller doesn't belong to, and an unknown project all come back as
// ErrNotFound, so an outsider probing a slug learns nothing; a member who
// merely lacks the role gets ErrForbidden — see org.Service.Resolve.
func (s *Service) Resolve(ctx context.Context, caller *user.User, orgSlug, projectSlug string, minRole org.Role) (*Project, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, minRole)
	if errors.Is(err, org.ErrForbidden) {
		// The caller is in this organization and knows it exists; they
		// just lack the role. Say so rather than pretending the project
		// isn't there.
		return nil, err
	}
	if err != nil {
		return nil, ErrNotFound
	}
	p, err := s.Repo().BySlug(ctx, o.ID, projectSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	return p, nil
}

// ResolveEnvironment is Resolve one level down.
func (s *Service) ResolveEnvironment(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string, minRole org.Role) (*Environment, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, minRole)
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
func (s *Service) Create(ctx context.Context, caller *user.User, orgSlug, projectSlug string) (*Project, *Environment, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, org.RoleAdmin)
	if err != nil {
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
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		var err error
		created, err = NewRepository(tx).Create(ctx, o.ID, projectSlug)
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
// created — organization, project, environment or app — because every
// one of them is a path component of an app's registry reference, which
// is derived on read rather than stored. Renaming one would silently
// move every app under it: pushes configured against the old path would
// start failing and images already pushed would be stranded where
// nothing looks for them again. The identifier is the one promise the
// daemon makes to whatever is configured against it.
func (s *Service) Update(ctx context.Context, caller *user.User, orgSlug, projectSlug string, description *string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return s.Repo().Update(ctx, p.ID, description)
}

func (s *Service) List(ctx context.Context, caller *user.User, orgSlug string) ([]*Project, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.Repo().ListForOrg(ctx, o.ID)
}

// Env returns the variables set on a project. Reading at RoleMember:
// anyone who can deploy an app needs to see what it will inherit.
func (s *Service) Env(ctx context.Context, caller *user.User, orgSlug, projectSlug string) (envvar.Map, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return p.Env, nil
}

// SetEnv replaces the project's full set of variables, deleting any key
// not present. Every environment and every app in the project inherits
// what remains.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug string, env envvar.Map) (*Project, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return p, s.Repo().SetEnv(ctx, p.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug string, set envvar.Map, unset []string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return p, s.Repo().MergeEnv(ctx, p.ID, set, unset)
}

// Delete removes a project and its environments. It is refused while any
// app still lives in it: deleting those means stopping containers, which
// is the app module's job and the operator's decision, one app at a time.
func (s *Service) Delete(ctx context.Context, caller *user.User, orgSlug, projectSlug string) (*Project, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	count, err := s.Repo().CountApps(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrHasApps
	}
	return p, s.db.WithTx(ctx, func(tx database.Queryer) error {
		return NewRepository(tx).Delete(ctx, p.ID)
	})
}

// CreateEnvironment adds an environment to an existing project.
func (s *Service) CreateEnvironment(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string) (*Environment, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
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

func (s *Service) ListEnvironments(ctx context.Context, caller *user.User, orgSlug, projectSlug string) ([]*Environment, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.EnvironmentRepo().ListForProject(ctx, p.ID)
}

// EnvironmentEnv returns the variables set on one environment, plus the
// effective set an app there would inherit — the project's, overridden
// by this environment's.
func (s *Service) EnvironmentEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string) (envvar.Map, []envvar.Resolved, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, nil, ErrEnvironmentNotFound
	}
	resolved := envvar.Resolve(
		envvar.Layer{Source: envvar.SourceProject, Vars: p.Env},
		envvar.Layer{Source: envvar.SourceEnvironment, Vars: e.Env},
	)
	return e.Env, resolved, nil
}

// SetEnvironmentEnv replaces one environment's full set of variables,
// deleting any key not present.
func (s *Service) SetEnvironmentEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string, env envvar.Map) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, orgSlug, projectSlug, envSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().SetEnv(ctx, e.ID, env)
}

// MergeEnvironmentEnv adds or overwrites the given variables and removes
// the unset ones, leaving every other key untouched.
func (s *Service) MergeEnvironmentEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string, set envvar.Map, unset []string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, orgSlug, projectSlug, envSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().MergeEnv(ctx, e.ID, set, unset)
}

// DeleteEnvironment removes an environment, refusing to delete
// production or one that still has apps in it.
// UpdateEnvironment changes an environment's description. Not
// its slug: unlike a project's, which the dashboard lets you rename
// with a warning, an environment's slug is the third component of every
// app reference under it and there is no equivalent screen for it yet.
// A nil field is left as it was.
func (s *Service) UpdateEnvironment(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string, description *string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, orgSlug, projectSlug, envSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return s.EnvironmentRepo().Update(ctx, e.ID, description)
}

func (s *Service) DeleteEnvironment(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, orgSlug, projectSlug, envSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if e.Slug == ProductionEnvSlug {
		return nil, ErrProductionUndeletable
	}
	count, err := s.EnvironmentRepo().CountApps(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrEnvironmentHasApps
	}
	return e, s.EnvironmentRepo().Delete(ctx, e.ID)
}
