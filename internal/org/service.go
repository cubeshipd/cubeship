package org

import (
	"context"
	"errors"

	"cubeship/internal/platform/database"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service holds the organization use cases, and — through Authorize — the
// authorization every other module defers to.
type Service struct {
	db    *database.DB
	users *user.Service
}

func NewService(db *database.DB, users *user.Service) *Service {
	return &Service{db: db, users: users}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Authorize reports whether caller may act on orgID at at least minRole.
//
// Super-admins are always authorized. An org admin satisfies both
// RoleAdmin and RoleMember checks; a member only satisfies RoleMember. A
// nil caller (unauthenticated) is never authorized.
//
// This is the one implementation of that question on the instance. Every
// other module reaches it through Resolve or through its own resource's
// owning organization.
func (s *Service) Authorize(ctx context.Context, caller *user.User, orgID int64, minRole Role) bool {
	if caller == nil {
		return false
	}
	if caller.IsSuperAdmin {
		return true
	}
	role, err := s.Repo().MembershipRole(ctx, caller.ID, orgID)
	if err != nil {
		return false
	}
	if minRole == RoleMember {
		return true
	}
	return role == RoleAdmin
}

// Resolve looks up an organization by slug and checks caller is
// authorized in it at minRole. It is what every other module calls to
// enter an organization's scope.
//
// The two refusals are deliberately different, and the distinction is the
// whole point:
//
//   - A caller who is not a member at all gets ErrNotFound, identical to
//     the answer for an organization that doesn't exist. Otherwise
//     anyone with a valid API key could enumerate the instance's tenants
//     by guessing slugs.
//   - A caller who IS a member but lacks the role gets ErrForbidden.
//     They already know the organization exists — hiding it from them
//     would only be confusing, and tells an attacker nothing new.
func (s *Service) Resolve(ctx context.Context, caller *user.User, orgSlug string, minRole Role) (*Organization, error) {
	o, err := s.Repo().BySlug(ctx, orgSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	if caller == nil {
		return nil, ErrNotFound
	}
	if caller.IsSuperAdmin {
		return o, nil
	}
	role, err := s.Repo().MembershipRole(ctx, caller.ID, o.ID)
	if err != nil {
		return nil, ErrNotFound
	}
	if minRole == RoleAdmin && role != RoleAdmin {
		return nil, ErrForbidden
	}
	return o, nil
}

// Create registers a new organization. Only a super-admin may: an
// organization is a tenant boundary, and handing that out would let any
// user mint namespaces the operator never approved.
func (s *Service) Create(ctx context.Context, caller *user.User, orgSlug string) (*Organization, error) {
	if caller == nil || !caller.IsSuperAdmin {
		return nil, ErrSuperAdminOnly
	}
	if !slug.Valid(orgSlug) {
		return nil, slug.ErrInvalid
	}
	created, err := s.Repo().Create(ctx, orgSlug)
	if database.IsUniqueViolation(err) {
		// The unique index decides, not a preceding lookup — see
		// database.IsUniqueViolation.
		return nil, ErrAlreadyExists
	}
	return created, err
}

// Delete removes an organization and its memberships. Super-admin only,
// like creating one, and refused while any project remains: an
// organization is a tenant boundary, and removing it out from under live
// projects would strand every app inside them.
func (s *Service) Delete(ctx context.Context, caller *user.User, orgSlug string) (*Organization, error) {
	if caller == nil || !caller.IsSuperAdmin {
		return nil, ErrSuperAdminOnly
	}
	o, err := s.Repo().BySlug(ctx, orgSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	count, err := s.Repo().CountProjects(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, ErrHasProjects
	}
	return o, s.db.WithTx(ctx, func(tx database.Queryer) error {
		return NewRepository(tx).Delete(ctx, o.ID)
	})
}

// List returns the organizations caller can see: all of them for a
// super-admin, and otherwise the ones they belong to.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Organization, error) {
	if caller == nil {
		return nil, user.ErrUnauthenticated
	}
	if caller.IsSuperAdmin {
		return s.Repo().List(ctx)
	}
	memberships, err := s.Repo().ListMembershipsForUser(ctx, caller.ID)
	if err != nil {
		return nil, err
	}
	out := make([]*Organization, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, &Organization{ID: m.OrgID, Slug: m.OrgSlug})
	}
	return out, nil
}

// AddUser adds username to org with role, creating the user (and their
// first API key) if this is the first organization they've been added to.
// An existing username gains a membership instead of colliding on the
// unique index — users belong to as many organizations as they are added
// to, each with its own role.
//
// The returned API key is empty when an existing user was added to a
// further organization: that user keeps the key they already have.
func (s *Service) AddUser(ctx context.Context, caller *user.User, o *Organization, username string, role Role) (apiKey string, err error) {
	if !role.Valid() {
		return "", ErrInvalidRole
	}
	if !s.Authorize(ctx, caller, o.ID, RoleAdmin) {
		return "", ErrForbidden
	}

	// One transaction for the whole thing: a user created without a
	// membership or a key would hold their username forever with no way
	// to finish or undo it through the API.
	err = s.db.WithTx(ctx, func(tx database.Queryer) error {
		users := user.NewRepository(tx)
		orgs := NewRepository(tx)

		existing, err := users.ByUsername(ctx, username)
		switch {
		case err == nil:
			if _, err := orgs.MembershipRole(ctx, existing.ID, o.ID); err == nil {
				return ErrAlreadyMember
			} else if !errors.Is(err, database.ErrNotFound) {
				return err
			}
			if err := orgs.AddMembership(ctx, existing.ID, o.ID, role); err != nil {
				// Two concurrent adds of the same user: the primary key
				// says what the lookup above could not.
				if database.IsUniqueViolation(err) {
					return ErrAlreadyMember
				}
				return err
			}
			return nil
		case !errors.Is(err, database.ErrNotFound):
			return err
		}

		created, key, err := s.users.CreateWithAPIKey(ctx, tx, username, false)
		if database.IsUniqueViolation(err) {
			// Another request created this username between the lookup
			// above and here. Both cannot own it; the loser is told so
			// rather than shown a driver error.
			return ErrUsernameTaken
		}
		if err != nil {
			return err
		}
		if err := orgs.AddMembership(ctx, created.ID, o.ID, role); err != nil {
			return err
		}
		apiKey = key
		return nil
	})
	if err != nil {
		return "", err
	}
	return apiKey, nil
}
