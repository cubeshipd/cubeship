package app

import (
	"context"
	"errors"
	"fmt"

	"cubeship/internal/org"
	"cubeship/internal/platform/dockerx"
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

	// SourceExternal is an image in a registry Cubeship does not run —
	// Docker Hub, GitHub, DigitalOcean, ECR. Nothing tells Cubeship when
	// one of those is pushed to, so these apps have no autodeploy: a
	// deploy is something you ask for.
	SourceExternal Source = "external"
)

// DefaultSource is what an app gets when none is named.
const DefaultSource = SourceRegistry

// Valid reports whether s is a source this version can deploy.
//
// Building from a repository is the remaining reason Source exists, and
// it is not implemented: accepting a value the daemon cannot act on
// would let someone create an app that can never deploy.
func (s Source) Valid() bool {
	return s == SourceRegistry || s == SourceExternal
}

// Builds reports whether deploying this source runs a build on the
// host.
//
// It is the question authorization asks. A build executes whatever the
// source contains, with the builder's privileges — that is a different
// kind of act from running an image someone already vetted, and it needs
// a different role. No source builds yet; the ones that will are a
// Dockerfile and a detected runtime.
func (s Source) Builds() bool {
	return false
}

// RoleToDeploy is the role a caller needs to deploy this source.
//
// Running a published image is what a member is for. Turning source into
// an image means executing it, so that is an admin's.
func RoleToDeploy(s Source) org.Role {
	if s.Builds() {
		return org.RoleAdmin
	}
	return org.RoleMember
}

// ErrUnknownSource reports a source this version cannot deploy.
var ErrUnknownSource = errors.New(`source must be "registry" or "external"`)

// ErrImageRequired reports an external app with nothing to pull.
var ErrImageRequired = errors.New("an external app needs the image it pulls, without a tag")

// ErrImageNotAllowed reports an image given for a source that derives
// its own.
var ErrImageNotAllowed = errors.New("only an external app names its image; a registry app derives one from its reference")

// ErrImageCarriesTag reports an image pinned to a tag. Which tag to run
// is what a deploy chooses.
var ErrImageCarriesTag = errors.New("give the image without a tag; the tag is chosen when you deploy")

// ImageSource produces something the daemon can run.
//
// The split matters. Check answers "could this app deploy at all right
// now", cheaply, so a misconfiguration is a refusal the caller sees
// instead of a deployment that fails minutes later. Resolve does the
// actual work, inside the detached deploy — which is where a source that
// builds will do its building.
type ImageSource interface {
	Check(ctx context.Context, a *Scoped) error
	Resolve(ctx context.Context, a *Scoped, tag string) (Image, error)
}

// Image is what a deploy pulls: a reference, and the credentials to
// reach it with when it lives somewhere Cubeship does not run.
//
// The two travel together because they are one answer. Resolving the
// reference is what determines which registry is involved, and so which
// login applies; splitting them would mean asking the same question
// twice and being able to disagree with yourself.
type Image struct {
	Ref  string
	Auth *dockerx.RegistryAuth
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
func (s registrySource) Resolve(ctx context.Context, a *Scoped, tag string) (Image, error) {
	host := s.registryHost(ctx)
	if host == "" {
		return Image{}, ErrNoRegistry
	}
	if tag == "" {
		tag = "latest"
	}
	// No Auth: the daemon reaches its own registry with a token it mints
	// itself, which dockerx attaches by host.
	return Image{Ref: LocalRegistryHost + "/" + ReferenceOf(a).String() + ":" + tag}, nil
}

// externalSource pulls an image somebody else's registry holds.
type externalSource struct {
	// credentials answers what login the app's organization holds for a
	// given image's registry, if any.
	credentials func(ctx context.Context, orgID int64, image string) (*dockerx.RegistryAuth, error)
}

// Check only confirms there is something to pull. Whether the
// credentials work, or the tag exists, is the registry's answer and only
// a real pull can ask it — refusing here on a guess would block deploys
// that would have worked.
func (externalSource) Check(_ context.Context, a *Scoped) error {
	if a.SourceImage == "" {
		return ErrImageRequired
	}
	return nil
}

func (s externalSource) Resolve(ctx context.Context, a *Scoped, tag string) (Image, error) {
	if a.SourceImage == "" {
		return Image{}, ErrImageRequired
	}
	if tag == "" {
		tag = "latest"
	}
	auth, err := s.credentials(ctx, a.OrgID, a.SourceImage)
	if err != nil {
		return Image{}, err
	}
	// A public image needs no login, so a missing credential is not a
	// failure: let the registry be the one to refuse.
	return Image{Ref: a.SourceImage + ":" + tag, Auth: auth}, nil
}

// sourceFor returns the ImageSource an app deploys through.
func (o *Orchestrator) sourceFor(a *Scoped) (ImageSource, error) {
	switch Source(a.Source) {
	case SourceRegistry:
		return registrySource{registryHost: o.registryHost}, nil
	case SourceExternal:
		return externalSource{credentials: o.registryCredentials}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, a.Source)
	}
}
