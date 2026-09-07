package settings

import (
	"context"
	"strings"

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

	// host answers what address this machine has. Set by the daemon,
	// which is the only thing that can reach outside its own container;
	// nil in a test and on a daemon that is itself a host process, where
	// the answer below it is already the right one.
	host HostAddress
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// OnChange registers what to run after a setting is written. Called once
// at startup, before anything serves.
func (s *Service) OnChange(fn func(context.Context, Values) error) {
	s.onChange = fn
}

// SetHostAddress wires in the thing that can ask the machine what its
// own address is. Called once at startup, like OnChange and for the
// same reason: only the daemon knows how to reach outside its container.
func (s *Service) SetHostAddress(h HostAddress) { s.host = h }

// PublicIP is what this instance's DNS records should point at, with
// every answer available to a running daemon rather than only the ones a
// pure function has.
//
// It is Values.PublicAddressFor with the host's own address slotted in
// **above** the daemon's: in a container the daemon's answer is a bridge
// address and is discarded anyway, and on a host process the two agree.
// Below the operator's own value and below the address the dashboard was
// opened at, both of which are evidence about how this instance is
// actually reached from outside.
//
// Empty is a real answer and means "this instance does not know". A
// caller about to write a DNS record has to stop there rather than
// write something.
func (s *Service) PublicIP(ctx context.Context, v Values, reachedAt string) string {
	if configured := strings.TrimSpace(v.Get(PublicIP)); configured != "" {
		return configured
	}
	if ip := ipOfHost(reachedAt); ip != "" {
		return ip
	}
	if s.host != nil {
		// Checked here rather than trusted: this is the last place
		// between a detected address and a DNS record, and an
		// implementation that answered with a bridge address would be
		// the whole bug back again.
		if ip := routable(s.host.Address(ctx)); ip != "" {
			return ip
		}
	}
	return outboundAddress()
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
