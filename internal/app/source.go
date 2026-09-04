package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

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

	// SourceDockerfile builds the app from a Dockerfile in a Git
	// repository. Cubeship clones and builds it, so what runs is code
	// this instance compiled rather than an artifact someone handed it.
	SourceDockerfile Source = "dockerfile"

	// SourceRailpack builds from a Git repository with no Dockerfile at
	// all: Railpack reads the repository, works out what it is, and
	// produces the build itself.
	SourceRailpack Source = "railpack"
)

// DefaultSource is what an app gets when none is named.
const DefaultSource = SourceRegistry

// Valid reports whether s is a source this version can deploy.
//
// Building from a repository is the remaining reason Source exists, and
// it is not implemented: accepting a value the daemon cannot act on
// would let someone create an app that can never deploy.
func (s Source) Valid() bool {
	switch s {
	case SourceRegistry, SourceExternal, SourceDockerfile, SourceRailpack:
		return true
	}
	return false
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
	return s == SourceDockerfile || s == SourceRailpack
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
var ErrUnknownSource = errors.New(`source must be "registry", "external", "dockerfile" or "railpack"`)

// ErrRepoRequired reports a building app with nothing to build.
var ErrRepoRequired = errors.New("a building app needs the Git repository it builds from")

// ErrRepoNotAllowed reports a repository given for a source that does
// not build.
var ErrRepoNotAllowed = errors.New("only a building app names a repository")

// ErrDockerfileNotAllowed reports a Dockerfile path given to a source
// that does not read one.
var ErrDockerfileNotAllowed = errors.New(`only a "dockerfile" app names a Dockerfile; Railpack works the build out itself`)

// ErrRepoNotSupported reports a repository URL the builder cannot
// fetch.
var ErrRepoNotSupported = errors.New("the repository must be an https://, http:// or git:// URL — ssh needs a key this instance does not have")

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
// logs is where a source that does work reports it. A source that only
// names an image writes nothing.
type ImageSource interface {
	Check(ctx context.Context, a *Scoped) error
	Resolve(ctx context.Context, a *Scoped, tag string, logs io.Writer) (Image, error)
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

	// Local says the image is already in the Engine's store, because
	// this deploy is what put it there. Pulling it would look for it in
	// a registry that has never heard of it.
	Local bool
}

// registrySource deploys what was pushed to the embedded registry.
type registrySource struct {
	// registryHost reports the public registry name, or "" while the
	// instance has no domain.
	registryHost func(ctx context.Context) string

	// localRegistry is where the daemon itself pulls from, which is
	// never the public name — see Resolve.
	localRegistry string
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
func (s registrySource) Resolve(ctx context.Context, a *Scoped, tag string, _ io.Writer) (Image, error) {
	host := s.registryHost(ctx)
	if host == "" {
		return Image{}, ErrNoRegistry
	}
	if tag == "" {
		tag = "latest"
	}
	// No Auth: the daemon reaches its own registry with a token it mints
	// itself, which dockerx attaches by host.
	return Image{Ref: s.localRegistry + "/" + ReferenceOf(a).String() + ":" + tag}, nil
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

func (s externalSource) Resolve(ctx context.Context, a *Scoped, tag string, _ io.Writer) (Image, error) {
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

// repositorySource builds the app from a repository, and hands back an
// image that is already local because building it is what created it.
// Which of the two ways it is built — a Dockerfile someone wrote, or a
// plan Railpack worked out — is decided further down, where the source
// is in hand.
type repositorySource struct {
	build func(ctx context.Context, a *Scoped, ref string, logs io.Writer) (string, error)
}

// Check confirms there is something to build. Whether the repository
// exists, the ref resolves or the Dockerfile compiles are the build's
// answers, and only a build can ask them.
func (repositorySource) Check(_ context.Context, a *Scoped) error {
	if a.SourceRepo == "" {
		return ErrRepoRequired
	}
	return nil
}

// Resolve is the build. It runs inside the detached deploy, which is
// what the Check/Resolve split is for: minutes of work with nobody
// holding a connection open.
func (s repositorySource) Resolve(ctx context.Context, a *Scoped, ref string, logs io.Writer) (Image, error) {
	if a.SourceRepo == "" {
		return Image{}, ErrRepoRequired
	}
	if ref == "" {
		ref = a.SourceRef
	}
	image, err := s.build(ctx, a, ref, logs)
	if err != nil {
		return Image{}, err
	}
	return Image{Ref: image, Local: true}, nil
}

// BuildImageName is what a build's result is called in the Engine's
// store. It is never pushed anywhere, so the name only has to be
// readable in `docker images` and unique per app and ref.
func BuildImageName(a *Scoped, ref string) string {
	if ref == "" {
		ref = "default"
	}
	return "cubeship-build/" + ReferenceOf(a).String() + ":" + sanitizeTag(ref)
}

// sanitizeTag reduces a Git ref to something Docker accepts as a tag:
// branches carry slashes, and a commit-ish can carry anything.
func sanitizeTag(ref string) string {
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "build"
	}
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

// sourceFor returns the ImageSource an app deploys through.
func (o *Orchestrator) sourceFor(a *Scoped) (ImageSource, error) {
	switch Source(a.Source) {
	case SourceRegistry:
		return registrySource{registryHost: o.registryHost, localRegistry: o.localRegistry}, nil
	case SourceExternal:
		return externalSource{credentials: o.registryCredentials}, nil
	case SourceDockerfile, SourceRailpack:
		return repositorySource{build: o.buildFromRepository}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, a.Source)
	}
}

// checkOrigin refuses, at creation, an app that could never deploy: one
// with no way to get an image, or one carrying a setting its source
// would silently ignore. Finding out at deploy time — minutes later,
// with nobody watching — is the alternative.
func checkOrigin(source Source, o *Origin) error {
	o.Image = strings.TrimSpace(o.Image)
	o.Repo = strings.TrimSpace(o.Repo)
	o.Ref = strings.TrimSpace(o.Ref)
	o.Dockerfile = strings.TrimSpace(o.Dockerfile)

	if source == SourceExternal {
		if o.Image == "" {
			return ErrImageRequired
		}
		if strings.ContainsAny(o.Image, " \t") || strings.Contains(o.Image, "://") {
			return ErrImageRequired
		}
		// The tag is the deploy's argument, not the app's identity: an
		// app pinned to one tag could never be told to run another.
		if _, tag, found := strings.Cut(path.Base(o.Image), ":"); found && tag != "" {
			return ErrImageCarriesTag
		}
	} else if o.Image != "" {
		return ErrImageNotAllowed
	}

	if source.Builds() {
		if o.Repo == "" {
			return ErrRepoRequired
		}
		// The rule is what the builder can fetch unaided, not what is
		// safe: ssh needs a key this instance does not have, and a clone
		// failing on a host key deep inside a build explains nothing.
		//
		// http:// and git:// are allowed because a private network with
		// a self-hosted Git server is a real way to run this. Neither
		// authenticates what comes back, and a build runs whatever comes
		// back — so on anything reachable from the internet, use https.
		if !strings.HasPrefix(o.Repo, "https://") &&
			!strings.HasPrefix(o.Repo, "http://") &&
			!strings.HasPrefix(o.Repo, "git://") {
			return ErrRepoNotSupported
		}
		// The ref belongs to the app or to the deploy, never to the
		// URL: two places to say which commit is one too many.
		if strings.Contains(o.Repo, "#") {
			return ErrRepoNotSupported
		}
	} else if o.Repo != "" || o.Ref != "" || o.Dockerfile != "" {
		return ErrRepoNotAllowed
	}

	// Only a Dockerfile build has a Dockerfile. Railpack works out the
	// build itself, and a path it would ignore is a setting someone
	// meant to have an effect.
	if source != SourceDockerfile && o.Dockerfile != "" {
		return ErrDockerfileNotAllowed
	}
	return nil
}
