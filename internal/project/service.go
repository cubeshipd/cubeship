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
func (s *Service) Create(ctx context.Context, caller *user.User, orgSlug, projectSlug, name string) (*Project, *Environment, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, org.RoleAdmin)
	if err != nil {
		return nil, nil, err
	}
	if !slug.Valid(projectSlug) {
		return nil, nil, slug.ErrInvalid
	}
	if _, err := s.Repo().BySlug(ctx, o.ID, projectSlug); err == nil {
		return nil, nil, ErrAlreadyExists
	}

	var created *Project
	var env *Environment
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		var err error
		created, err = NewRepository(tx).Create(ctx, o.ID, projectSlug, name)
		if err != nil {
			return err
		}
		env, err = NewEnvironmentRepository(tx).Create(ctx, created.ID, ProductionEnvSlug, "Production")
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return created, env, nil
}

func (s *Service) List(ctx context.Context, caller *user.User, orgSlug string) ([]*Project, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.Repo().ListForOrg(ctx, o.ID)
}

// SetEnv replaces the project's full set of variables. Every environment
// and every app in the project inherits them.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug string, env envvar.Map) (*Project, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return p, s.Repo().SetEnv(ctx, p.ID, env)
}

// CreateEnvironment adds an environment to an existing project.
func (s *Service) CreateEnvironment(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug, name string) (*Environment, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if !slug.Valid(envSlug) {
		return nil, slug.ErrInvalid
	}
	if _, err := s.EnvironmentRepo().BySlug(ctx, p.ID, envSlug); err == nil {
		return nil, ErrEnvironmentExists
	}
	return s.EnvironmentRepo().Create(ctx, p.ID, envSlug, name)
}

func (s *Service) ListEnvironments(ctx context.Context, caller *user.User, orgSlug, projectSlug string) ([]*Environment, error) {
	p, err := s.Resolve(ctx, caller, orgSlug, projectSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.EnvironmentRepo().ListForProject(ctx, p.ID)
}

// SetEnvironmentEnv replaces one environment's full set of variables.
func (s *Service) SetEnvironmentEnv(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug string, env envvar.Map) (*Environment, error) {
	e, err := s.ResolveEnvironment(ctx, caller, orgSlug, projectSlug, envSlug, org.RoleAdmin)
	if err != nil {
		return nil, err
	}
	return e, s.EnvironmentRepo().SetEnv(ctx, e.ID, env)
}

// DeleteEnvironment removes an environment, refusing to delete
// production or one that still has apps in it.
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
