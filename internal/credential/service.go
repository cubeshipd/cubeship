package credential

import (
	"context"
	"strings"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Managing credentials is an admin's job, reading them included. A
// member can deploy an app that pulls through one — they just never see
// the list, because the list is the map of what this instance can reach.
const manageRole = user.RoleAdmin

// Dependant is a module that may be using a credential.
//
// The seam runs this way so table ownership does not have to bend: this
// module cannot read `external_registries` or `settings` to work out
// what would break, and it should not — those are somebody else's rows.
// They answer the question instead, and the daemon wires them in.
//
// A credential with no dependants wired is a credential that deletes
// silently, so Service refuses to delete at all until they are — the
// same refusal project makes when its teardown is missing.
type Dependant interface {
	UsesCredential(ctx context.Context, id int64) ([]Use, error)
}

// Service holds the credential use cases.
type Service struct {
	db *database.DB
	// dependants are the modules asked what would break before a
	// delete. Nil until the daemon wires them.
	dependants []Dependant
	wired      bool
}

func NewService(db *database.DB) *Service { return &Service{db: db} }

// SetDependants wires in the modules that use credentials. Called once,
// at startup, by the only package that knows every module exists.
func (s *Service) SetDependants(d ...Dependant) {
	s.dependants = d
	s.wired = true
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Create stores a secret under a label.
//
// It asks for nothing else. A credential is a facilitator — see the
// package comment — so what shape the secret has is the *use*'s
// question, and this module refusing a token with no username would be
// this module deciding in advance what the token may be used for.
func (s *Service) Create(ctx context.Context, caller *user.User, in Credential) (*Credential, error) {
	return s.CreateWith(ctx, caller, s.db, in)
}

// CreateWith is Create on a given Queryer, so a module that stores a
// credential as part of a larger act can do both in one transaction.
//
// That is what keeps a credential from being a *prerequisite*. A
// registry added with a login typed in place of choosing a stored
// account is one request: the account is created and the registry is
// created, or neither is. Without this the caller would have to make
// the credential first and be left holding an orphan when the registry
// turned out to be unreachable.
func (s *Service) CreateWith(ctx context.Context, caller *user.User, q database.Queryer, in Credential) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		return nil, ErrLabelRequired
	}
	in.Username = strings.TrimSpace(in.Username)
	if strings.TrimSpace(in.Password) == "" {
		return nil, ErrPasswordRequired
	}

	created, err := NewRepository(q).Create(ctx, &in)
	if database.IsUniqueViolation(err) {
		// The index decides, not a preceding lookup: two concurrent
		// creates would both pass a check and the loser would surface
		// as a 500.
		return nil, ErrLabelTaken
	}
	return created, err
}

// Update changes the label, the first half and the secret. A nil field
// is left alone, so renaming one cannot blank its password.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, label, username, password *string) (*Credential, error) {
	if _, err := s.Resolve(ctx, caller, id); err != nil {
		return nil, err
	}
	if label != nil {
		trimmed := strings.TrimSpace(*label)
		if trimmed == "" {
			return nil, ErrLabelRequired
		}
		label = &trimmed
	}
	if username != nil {
		trimmed := strings.TrimSpace(*username)
		username = &trimmed
	}
	if password != nil && strings.TrimSpace(*password) == "" {
		return nil, ErrPasswordRequired
	}

	updated, err := s.Repo().Update(ctx, id, label, username, password)
	if database.IsUniqueViolation(err) {
		return nil, ErrLabelTaken
	}
	return updated, err
}

// List is every credential this instance holds.
//
// Unfiltered, because there is nothing to filter by: a credential is
// not for one job. Every screen that offers a choice of one offers all
// of them, and the use it is being wired to is what decides whether it
// works.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

// Resolve looks one up and requires the caller's role.
func (s *Service) Resolve(ctx context.Context, caller *user.User, id int64) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	c, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return c, nil
}

// Get reads a credential with no caller, for a module that has already
// authorized the thing the credential is being used *for* — a deploy
// pulling an image, the daemon writing its own DNS record. Nothing that
// takes an id from a request may use it.
func (s *Service) Get(ctx context.Context, id int64) (*Credential, error) {
	c, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return c, nil
}

// Uses is what would break if this credential went.
func (s *Service) Uses(ctx context.Context, id int64) ([]Use, error) {
	var uses []Use
	for _, d := range s.dependants {
		found, err := d.UsesCredential(ctx, id)
		if err != nil {
			return nil, err
		}
		uses = append(uses, found...)
	}
	return uses, nil
}

// Delete removes a credential, and refuses while anything still
// authenticates with it.
//
// Refused rather than cascaded, and rather than orphaned: a registry
// whose login vanished is a registry that cannot log in, and the way
// that surfaces is a deploy failing minutes later with nobody watching.
// The refusal names what is using it, because "in use" that does not
// say by what is a refusal somebody has to go hunting to satisfy.
func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if _, err := s.Resolve(ctx, caller, id); err != nil {
		return err
	}
	if !s.wired {
		// Nothing has said what depends on this, so nothing can say it
		// is safe. Refusing is the only honest answer — see Dependant.
		return ErrNoDependants
	}
	uses, err := s.Uses(ctx, id)
	if err != nil {
		return err
	}
	if len(uses) > 0 {
		return InUseError(uses)
	}
	return s.Repo().Delete(ctx, id)
}
