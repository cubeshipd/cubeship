package app

import (
	"context"
	"errors"
	"fmt"
)

// Source says where an app's image comes from.
//
// It is the discriminator behind ImageSource: every app answers "how do I
// get an image to run" the same way, and the answer differs by source
// rather than by special cases scattered through the deploy path.
type Source string

const (
	// SourceRegistry is an image pushed to Cubeship's own registry. A
	// push is what triggers the deploy.
	SourceRegistry Source = "registry"
)

// DefaultSource is what an app gets when none is named.
const DefaultSource = SourceRegistry

// Valid reports whether s is a source this version can deploy.
//
// Building from a repository and pulling from a third-party registry are
// the reason Source exists, but neither is implemented: accepting a value
// the daemon cannot act on would let someone create an app that can never
// deploy.
func (s Source) Valid() bool {
	return s == SourceRegistry
}

// ErrUnknownSource reports a source this version cannot deploy.
var ErrUnknownSource = errors.New(`source must be "registry"`)

// ImageSource produces something the daemon can run.
//
// The split matters. Check answers "could this app deploy at all right
// now", cheaply, so a misconfiguration is a refusal the caller sees
// instead of a deployment that fails minutes later. Resolve does the
// actual work, inside the detached deploy — which is where a source that
// builds will do its building.
type ImageSource interface {
	Check(ctx context.Context, a *Scoped) error
	Resolve(ctx context.Context, a *Scoped, tag string) (string, error)
}

// registrySource deploys what was pushed to the embedded registry.
type registrySource struct {
	// registryHost reports the public registry name, or "" while the
	// instance has no domain.
	registryHost func(ctx context.Context) string
}

func (s registrySource) Check(ctx context.Context, _ *Scoped) error {
	if s.registryHost(ctx) == "" {
		return ErrNoRegistry
	}
	return nil
}

// Resolve returns the reference the daemon itself can pull: the
// loopback-published registry, not the public name. Pulling the public
// name would hairpin out to the VPS's own address and require a
// certificate to already exist, which must never block a deploy.
func (s registrySource) Resolve(ctx context.Context, a *Scoped, tag string) (string, error) {
	host := s.registryHost(ctx)
	if host == "" {
		return "", ErrNoRegistry
	}
	if tag == "" {
		tag = "latest"
	}
	return LocalRegistryHost + "/" + ReferenceOf(a).String() + ":" + tag, nil
}

// sourceFor returns the ImageSource an app deploys through.
func (o *Orchestrator) sourceFor(a *Scoped) (ImageSource, error) {
	switch Source(a.Source) {
	case SourceRegistry:
		return registrySource{registryHost: o.registryHost}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, a.Source)
	}
}
