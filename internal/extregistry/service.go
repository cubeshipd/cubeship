package extregistry

import (
	"context"
	"errors"

	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service holds the use cases for registry credentials. Both the HTTP
// handlers and the deploy path call exactly these.
type Service struct {
	db   *database.DB
	orgs *org.Service
}

func NewService(db *database.DB, orgs *org.Service) *Service {
	return &Service{db: db, orgs: orgs}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Managing credentials is an admin's job. A member can deploy an app
// that uses one — they just cannot read, add or change the login.
const manageRole = org.RoleAdmin

func (s *Service) Create(ctx context.Context, caller *user.User, orgSlug, name, host, username, password string) (*Credential, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, ErrNameRequired
	}
	if host = NormalizeHost(host); host == "" {
		return nil, ErrHostRequired
	}
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}

	c, err := s.Repo().Create(ctx, o.ID, name, host, username, password)
	if database.IsUniqueViolation(err) {
		// Two unique indexes, and the caller needs to know which one
		// they hit: renaming and re-pointing are different fixes.
		if existing, found, _ := s.Repo().ByHost(ctx, o.ID, host); found && existing != nil {
			return nil, ErrHostTaken
		}
		return nil, ErrNameTaken
	}
	return c, err
}

// Update replaces a credential's login, keeping the host it is for. A
// registry that has to be re-pointed at a different host is a different
// credential — delete it and add the new one, so no app silently starts
// authenticating somewhere else.
func (s *Service) Update(ctx context.Context, caller *user.User, orgSlug string, id int64, username, password string) (*Credential, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	if username == "" {
		return nil, ErrUsernameRequired
	}
	if password == "" {
		return nil, ErrPasswordRequired
	}
	c, err := s.Repo().Update(ctx, id, o.ID, username, password)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Service) List(ctx context.Context, caller *user.User, orgSlug string) ([]*Credential, error) {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return nil, err
	}
	return s.Repo().List(ctx, o.ID)
}

func (s *Service) Delete(ctx context.Context, caller *user.User, orgSlug string, id int64) error {
	o, err := s.orgs.Resolve(ctx, caller, orgSlug, manageRole)
	if err != nil {
		return err
	}
	err = s.Repo().Delete(ctx, id, o.ID)
	if errors.Is(err, database.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// ForImage returns the credential an organization holds for whichever
// registry image names, if it holds one.
//
// It runs inside a deploy, on the daemon's behalf rather than a
// caller's, so it does not authorize: by the time a deploy is running,
// the right to deploy that app has already been settled.
func (s *Service) ForImage(ctx context.Context, orgID int64, image string) (*Credential, bool, error) {
	return s.Repo().ByHost(ctx, orgID, HostOf(image))
}
