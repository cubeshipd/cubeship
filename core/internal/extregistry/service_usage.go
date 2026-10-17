package extregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/user"

	"github.com/distribution/reference"
)

// ErrNoListing is a registry that cannot say what it holds.
//
// Not every one can. The Docker Registry v2 API defines a catalogue
// endpoint, and the two biggest public registries — Docker Hub and
// GitHub's — disable it. That is their answer, not a failure here, and
// saying so beats an empty list that reads as "you have nothing".
var ErrNoListing = errors.New("this registry does not list what it holds")

// Repositories lists what a credential's registry contains.
func (s *Service) Repositories(ctx context.Context, caller *user.User, id int64) ([]Repo, error) {
	c, err := s.resolve(ctx, caller, id, user.LevelView)
	if err != nil {
		return nil, err
	}
	switch c.Provider {
	case ProviderAWS:
		return listECRRepositories(ctx, s.client, c)
	case ProviderDigitalOcean:
		return listDORepositories(ctx, s.client, c)
	}
	return listV2Repositories(ctx, s.client, c)
}

// Images lists one repository's tags.
func (s *Service) Images(ctx context.Context, caller *user.User, id int64, repository string) ([]Image, error) {
	c, err := s.resolve(ctx, caller, id, user.LevelView)
	if err != nil {
		return nil, err
	}
	if repository == "" {
		return nil, fmt.Errorf("name the repository to list")
	}
	switch c.Provider {
	case ProviderAWS:
		return listECRImages(ctx, s.client, c, repository)
	case ProviderDigitalOcean:
		return listDOImages(ctx, s.client, c, repository)
	}
	return listV2ImagesWithDates(ctx, s.client, c, repository)
}

// resolve finds one of this instance's credentials.
func (s *Service) resolve(ctx context.Context, caller *user.User, id int64, need user.Level) (*Credential, error) {
	if err := user.Allow(caller, user.ResRegistries, need, ""); err != nil {
		return nil, err
	}
	creds, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range creds {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, ErrNotFound
}

// DeleteImage removes one tag from a repository.
//
// Whether it frees anything is the registry's business: ECR reclaims the
// storage, and a registry that only untags does not. What is promised
// here is that nothing can pull that tag afterwards.
func (s *Service) DeleteImage(ctx context.Context, caller *user.User, id int64, repository string, ref ImageRef) error {
	c, err := s.resolve(ctx, caller, id, user.LevelManage)
	if err != nil {
		return err
	}
	if repository == "" || !ref.Named() {
		return fmt.Errorf("name the repository, and the tag or digest to delete")
	}
	switch c.Provider {
	case ProviderAWS:
		return deleteECRImage(ctx, s.client, c, repository, ref)
	case ProviderDigitalOcean:
		return deleteDOImage(ctx, s.client, c, repository, ref)
	}
	return deleteV2Image(ctx, s.client, c, repository, ref)
}

// DeleteRepository removes a repository and everything in it.
func (s *Service) DeleteRepository(ctx context.Context, caller *user.User, id int64, repository string) error {
	c, err := s.resolve(ctx, caller, id, user.LevelManage)
	if err != nil {
		return err
	}
	if repository == "" {
		return fmt.Errorf("name the repository to delete")
	}
	switch c.Provider {
	case ProviderAWS:
		return deleteECRRepository(ctx, s.client, c, repository)
	case ProviderDigitalOcean:
		return deleteDORepository(ctx, s.client, c, repository)
	}
	return deleteV2Repository(ctx, s.client, c, repository)
}

// Usage measures what a registry's images add up to.
//
// It is one call per repository — ECR has no aggregate to ask for — so
// it is its own endpoint rather than part of the listing: a page that
// waited for this before showing anything would wait for all of them.
func (s *Service) Usage(ctx context.Context, caller *user.User, id int64) (*Usage, error) {
	c, err := s.resolve(ctx, caller, id, user.LevelView)
	if err != nil {
		return nil, err
	}
	switch c.Provider {
	case ProviderDigitalOcean:
		return doUsage(ctx, s.client, c)
	case ProviderGeneric:
		return usageV2(ctx, s.client, c)
	}
	repos, err := listECRRepositories(ctx, s.client, c)
	if err != nil {
		return nil, err
	}
	return ecrUsage(ctx, s.client, c, repos)
}

// State is what a probe of a registry found. The three answers are three
// different jobs for whoever reads them: nothing, re-authenticate, or
// wait for someone else's registry to come back.
type State string

const (
	StateAvailable    State = "available"
	StateUnauthorized State = "unauthorized"
	StateUnreachable  State = "unreachable"
)

// Status is one registry's answer, and why.
type Status struct {
	State State `json:"state"`
	// Detail is the reason, for anything but available.
	Detail string `json:"detail,omitempty"`
}

// Probe asks a registry whether this login still works.
//
// It is a live call rather than something recorded at creation, because
// the interesting case is a credential that used to work: an access key
// revoked in AWS, a token expired at DigitalOcean, a password rotated
// somewhere else. None of those tell Cubeship anything — the first sign
// is a deploy failing to pull, and the point of asking now is to find
// out before that.
//
// For AWS the probe deliberately bypasses the token cache: a cached
// token stays valid for hours after the access key that minted it was
// deleted, so answering from it would report a dead login as healthy.
func (s *Service) Probe(ctx context.Context, caller *user.User, id int64) (*Status, error) {
	c, err := s.resolve(ctx, caller, id, user.LevelView)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	switch c.Provider {
	case ProviderAWS:
		_, err = getECRAuthorization(ctx, s.client, c.Username, c.Password, c.Region)
	case ProviderDigitalOcean:
		err = pingDO(ctx, s.client, c)
	default:
		err = pingV2(ctx, s.client, c)
	}
	if err == nil {
		return &Status{State: StateAvailable}, nil
	}
	if errors.Is(err, errUnauthorized) || errors.Is(err, errAWSUnauthorized) {
		return &Status{State: StateUnauthorized, Detail: err.Error()}, nil
	}
	return &Status{State: StateUnreachable, Detail: err.Error()}, nil
}

// probeTimeout bounds a probe. A dashboard row is waiting on it, and a
// registry that has not answered in this long is not available whatever
// it would eventually have said.
const probeTimeout = 10 * time.Second

// checkNamespace refuses a namespace on a provider that has none.
//
// ECR's host carries the account and there is nothing between it and
// the image; a generic registry's path is whatever the image says. Only
// DigitalOcean has a name sitting in the middle that someone types, so
// only DigitalOcean has one to correct.
func checkNamespace(ctx context.Context, s *Service, caller *user.User, id int64, namespace string) (string, error) {
	c, err := s.resolve(ctx, caller, id, user.LevelView)
	if err != nil {
		return "", err
	}
	if c.Provider != ProviderDigitalOcean {
		return "", fmt.Errorf("%w: only a DigitalOcean login has a registry name", ErrNamespaceRequired)
	}

	namespace = strings.Trim(strings.TrimSpace(namespace), "/")
	if namespace == "" {
		return "", ErrNamespaceRequired
	}
	if strings.ContainsAny(namespace, "/ ") {
		return "", fmt.Errorf("%w: a DigitalOcean registry name is one path segment", ErrNamespaceRequired)
	}
	return namespace, nil
}

// RepositoryOf reduces an image reference to what a registry's own API
// calls a repository: the part after the host, with Docker Hub's
// `library/` put back for an official image.
//
// `nginx` is the case that needs it. The reference has no host and one
// path component, and Docker Hub's v2 API has no such repository — the
// image is `library/nginx` there, and asking for `nginx` is a 404 that
// reads as "no such image" rather than as a missing prefix.
func RepositoryOf(image string) string {
	image = strings.TrimSpace(image)
	if i := strings.Index(image, "/"); i >= 0 {
		if first := image[:i]; strings.ContainsAny(first, ".:") || first == "localhost" {
			image = image[i+1:]
		}
	}
	if HostOf(image) == DockerHub && !strings.Contains(image, "/") && image != "" {
		return "library/" + image
	}
	return image
}

// TagsFor lists the tags one image reference could be deployed at,
// whether or not this instance holds a login for where it lives.
//
// It exists because the registries an app can pull from are not the
// same set as the registries this instance has rows for: a public image
// needs no credential, which is what makes running one the single thing
// a fresh install can do. Asking through a credential id could not
// describe that case at all.
//
// **A member's, unlike listing what a stored registry holds.** That
// listing is the instance's inventory — what secrets it holds and what
// is behind them — and this is one repository somebody already named.
// Anyone who may configure an app's source may already deploy any tag
// from it, so refusing to enumerate them would hide nothing and only
// make the field a guess.
func (s *Service) TagsFor(ctx context.Context, caller *user.User, image string) ([]Image, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	if _, err := reference.ParseNormalizedNamed(image); err != nil {
		return nil, fmt.Errorf("name a valid image to list tags for")
	}
	repository := RepositoryOf(image)
	if repository == "" {
		return nil, fmt.Errorf("name the image to list tags for")
	}

	c, found, err := s.ForImage(ctx, image)
	if err != nil {
		return nil, err
	}
	client := s.client
	if !found {
		// No login, which is not a failure: a public repository answers
		// an anonymous token request, and the v2 client already asks
		// for one when it holds no password.
		c = &Credential{Host: HostOf(image), Provider: ProviderGeneric}
		client = s.publicClient
	}
	switch c.Provider {
	case ProviderAWS:
		return listECRImages(ctx, s.client, c, repository)
	case ProviderDigitalOcean:
		return listDOImages(ctx, s.client, c, repository)
	}
	return listV2ImagesWithDates(ctx, client, c, repository)
}

func listV2ImagesWithDates(ctx context.Context, client *http.Client, c *Credential, repository string) ([]Image, error) {
	images, err := listV2Images(ctx, client, c, repository)
	if err == nil && c.Host == "ghcr.io" {
		ghcrPublishedAt(ctx, client, c.Password, repository, images)
		ghcrCreatedAt(ctx, client, c, repository, images)
	}
	return images, err
}
