package settings

import (
	"context"
	"strconv"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service reads and writes instance configuration.
//
// Changing a setting changes what the daemon's infrastructure containers
// should look like — a domain gives the registry somewhere to be, a
// contact address gives Traefik a certificate resolver — so a write
// notifies whoever is responsible for reconciling those.
type Service struct {
	db *database.DB

	// onChange re-applies the infrastructure after a write. It is set by
	// the daemon, which owns bootstrapping; nil in tests, where there is
	// nothing to reconcile.
	onChange func(context.Context, Values) error
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// OnChange registers what to run after a setting is written. Called once
// at startup, before anything serves.
func (s *Service) OnChange(fn func(context.Context, Values) error) {
	s.onChange = fn
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// All returns every setting. Readable by any authenticated caller: the
// dashboard needs to know whether a domain is configured to tell someone
// where to push, and none of these values is a secret.
func (s *Service) All(ctx context.Context, caller *user.User) (Values, error) {
	if caller == nil {
		return nil, user.ErrUnauthenticated
	}
	return s.Repo().All(ctx)
}

// Load reads the settings with no caller, for the daemon's own startup.
func (s *Service) Load(ctx context.Context) (Values, error) {
	return s.Repo().All(ctx)
}

// Set writes settings and re-applies the infrastructure they describe.
// Super-admin only: this is the VPS's configuration, not an
// organization's.
//
// The whole map is applied, then the reconcile runs once — setting a
// domain and a contact address together should rebuild Traefik once, not
// twice.
func (s *Service) Set(ctx context.Context, caller *user.User, values map[string]string) (Values, error) {
	if caller == nil || !caller.Is(user.RoleAdmin) {
		return nil, ErrSuperAdminOnly
	}
	for key := range values {
		if _, ok := Describe(key); !ok {
			return nil, ErrUnknownKey
		}
	}

	if err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		repo := NewRepository(tx)
		for key, value := range values {
			if err := repo.Set(ctx, key, value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	current, err := s.Repo().All(ctx)
	if err != nil {
		return nil, err
	}
	if s.onChange != nil {
		// A failure here leaves the setting stored but the
		// infrastructure stale, which is the right way round: the value
		// the operator asked for is kept, and the next start applies it.
		if err := s.onChange(ctx, current); err != nil {
			return current, err
		}
	}
	return current, nil
}

// SeedFromEnv writes values that have never been set, for an install
// upgrading from the release where these were environment variables. It
// never overwrites what is already there.
func (s *Service) SeedFromEnv(ctx context.Context, values map[string]string) error {
	repo := s.Repo()
	for key, value := range values {
		if value == "" {
			continue
		}
		if _, err := repo.SetIfUnset(ctx, key, value); err != nil {
			return err
		}
	}
	return nil
}

// UsesCredential reports whether this instance writes its own DNS
// records through a credential. See credential.Dependant: this module
// owns the setting, so it is the one that can answer.
func (s *Service) UsesCredential(ctx context.Context, id int64) ([]credential.Use, error) {
	values, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	if values.Get(DNSProviderID) != strconv.FormatInt(id, 10) {
		return nil, nil
	}
	return []credential.Use{{Kind: "the instance's own DNS"}}, nil
}
