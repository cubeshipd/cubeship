package app

import (
	"context"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
	"errors"
)

// AddDomain gives an app another name to answer at.
//
// The host is normalised the way a browser sends one — lowercase, no
// trailing dot — because that is what Traefik matches against, and a
// name stored differently is a name that never matches.
//
// Port 0 means DefaultPort. See Domain.Port for why nothing reads it
// off the image.
func (s *Service) AddDomain(ctx context.Context, caller *user.User, ref Reference, host string, port int) (*Scoped, error) {
	a, err := s.resolveDomains(ctx, caller, ref)
	if err != nil {
		return nil, err
	}

	host = NormalizeHost(host)
	if host == "" {
		return nil, ErrHostRequired
	}
	if !ValidHost(host) {
		return nil, ErrBadHost
	}
	if taken, err := s.instanceOwnsHost(ctx, host); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrHostIsTheInstance
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}

	if _, err := s.Repo().AddDomain(ctx, a.ID, host, port); err != nil {
		// The unique index is the authority. Two apps answering at one
		// name would give Traefik two answers, and which it picked
		// would be a detail of label ordering.
		if database.IsUniqueViolation(err) {
			return nil, ErrDomainTaken
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

// instanceOwnsHost reports whether a name is one the daemon already
// routes to itself.
//
// The domain is the dashboard and the API; registry.<domain> is the
// registry. Both get Traefik routers of their own, and a router the
// unique index knows nothing about is exactly the collision it cannot
// catch — the app would be created, and one of the two would quietly
// stop answering after a deploy.
// HostTaken reports whether AddDomain would refuse host for already
// being somebody's: an app's name, or one of the instance's own. For a
// caller adding several names at once, which has to know before it adds
// the first.
func (s *Service) HostTaken(ctx context.Context, host string) (bool, error) {
	host = NormalizeHost(host)
	if own, err := s.instanceOwnsHost(ctx, host); err != nil || own {
		return own, err
	}
	return s.Repo().HostTaken(ctx, host)
}

func (s *Service) instanceOwnsHost(ctx context.Context, host string) (bool, error) {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return false, err
	}
	domain := settings.APIHostFor(values.Get(settings.Domain))
	if domain == "" {
		return false, nil
	}
	return host == NormalizeHost(domain) ||
		host == NormalizeHost(settings.RegistryHostFor(values.Get(settings.Domain))), nil
}

// SetDomainPort changes what one of an app's names reaches.
func (s *Service) SetDomainPort(ctx context.Context, caller *user.User, ref Reference, domainID int64, port int) (*Scoped, error) {
	a, err := s.resolveDomains(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}
	if err := s.Repo().SetDomainPort(ctx, a.ID, domainID, port); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

// RemoveDomain takes a name off an app.
//
// The container keeps the labels it was created with, so the name goes
// on being served until the app is redeployed. That is the same rule
// every other routing change follows, and it is why this does not stop
// anything by itself.
func (s *Service) RemoveDomain(ctx context.Context, caller *user.User, ref Reference, domainID int64) (*Scoped, error) {
	a, err := s.resolveDomains(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	if err := s.Repo().RemoveDomain(ctx, a.ID, domainID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.LevelView)
}

// resolveDomains is an app whose names the caller may change.
func (s *Service) resolveDomains(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return nil, err
	}
	err = user.Allow(caller, user.ResDomains, user.LevelManage, ref.Project)
	if errors.Is(err, user.ErrHidden) {
		return nil, ErrNotFound
	}
	return a, err
}

// ShellTarget is the copy of an app a shell opens in, for a caller who
// may open one there.
//
// Naming no server takes the first copy that is running, rather than
// the first copy — a shell needs a process to join, and landing on a
// machine where the app is still being placed would refuse somebody
// whose app is plainly up somewhere.
func (s *Service) ShellTarget(ctx context.Context, caller *user.User, ref Reference, server string) (Replica, error) {
	a, err := s.Resolve(ctx, caller, ref, user.LevelView)
	if err != nil {
		return Replica{}, err
	}
	if err := user.AllowShell(caller, user.ResApps, ref.Project); err != nil {
		if errors.Is(err, user.ErrHidden) {
			return Replica{}, ErrNotFound
		}
		return Replica{}, err
	}
	if server == "" {
		for _, r := range a.Replicas {
			if r.Running() {
				return r, nil
			}
		}
		return Replica{}, ErrNoContainer
	}
	r, err := a.pick(server)
	if err != nil {
		return Replica{}, err
	}
	if !r.Running() {
		return Replica{}, ErrNoContainer
	}
	return r, nil
}
