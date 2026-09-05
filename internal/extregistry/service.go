package extregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service holds the use cases for registry credentials. Both the HTTP
// handlers and the deploy path call exactly these.
type Service struct {
	db *database.DB

	// client talks to a provider's API — AWS's, so far. Only a
	// credential whose token has to be fetched needs one.
	client *http.Client
	// tokens holds fetched registry logins until shortly before they
	// expire. An ECR token lasts twelve hours and a pull takes seconds
	// of that; fetching one per pull would be a signed round trip
	// against a rate limit for nothing.
	tokens sync.Map // credential id -> fetchedLogin
}

func NewService(db *database.DB) *Service {
	return &Service{
		db:     db,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// fetchedLogin is a login obtained from a provider, and when it stops
// working.
type fetchedLogin struct {
	username string
	password string
	expires  time.Time
}

// tokenMargin is how long before expiry a cached login stops being
// offered. A pull that starts inside the margin still finishes.
const tokenMargin = 10 * time.Minute

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Managing credentials is an admin's job. A member can deploy an app
// that uses one — they just cannot read, add or change the login.
const manageRole = user.RoleAdmin

// Create adds a login. What it asks for depends on the provider, and so
// does what it does with it: a generic registry is taken at its word,
// and an AWS one is used immediately — the call that proves the key
// works is also the call that says where the registry is.
func (s *Service) Create(ctx context.Context, caller *user.User, in Credential) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if !in.Provider.Valid() {
		return nil, ErrUnknownProvider
	}
	if in.Username == "" {
		return nil, ErrUsernameRequired
	}
	if in.Password == "" {
		return nil, ErrPasswordRequired
	}

	switch in.Provider {
	case ProviderDigitalOcean:
		// The host never varies; the registry's name is a path segment.
		// Asking for a URL would be asking someone to retype a constant
		// and get the rest wrong.
		if in.Namespace = strings.Trim(strings.TrimSpace(in.Namespace), "/"); in.Namespace == "" {
			return nil, ErrNamespaceRequired
		}
		in.Host = DigitalOceanHost

	case ProviderAWS:
		if in.Region = strings.TrimSpace(in.Region); in.Region == "" {
			return nil, ErrRegionRequired
		}
		// The host carries the account id, so it is discovered rather
		// than asked for — and discovering it is the same call that
		// proves the key can read a registry at all. A key that cannot
		// is better refused here than at a deploy.
		auth, err := getECRAuthorization(ctx, s.client, in.Username, in.Password, in.Region)
		if err != nil {
			return nil, err
		}
		in.Host = auth.Registry
		in.Namespace = ""

	default:
		if in.Host = NormalizeHost(in.Host); in.Host == "" {
			return nil, ErrHostRequired
		}
		in.Namespace = ""
		in.Region = ""
	}

	c, err := s.Repo().Create(ctx, in)
	if database.IsUniqueViolation(err) {
		return nil, ErrHostTaken
	}
	return c, err
}

// Update replaces a credential's login, keeping the host it is for. A
// registry that has to be re-pointed at a different host is a different
// credential — delete it and add the new one, so no app silently starts
// authenticating somewhere else.
// Update rotates the login, and for a provider whose namespace is a
// name rather than something derived, corrects that too.
//
// The host is still not editable — an app's pulls are matched to a
// credential by host, so re-pointing one in place would silently send
// them somewhere else. A namespace is different in kind: it is typed by
// hand, it is wrong in exactly one way, and until now the only way to
// fix a typo was to delete the login and enter the password again.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, username, password string, namespace *string) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}

	if namespace != nil {
		normalized, err := checkNamespace(ctx, s, caller, id, *namespace)
		if err != nil {
			return nil, err
		}
		namespace = &normalized
	}

	// A login is replaced as a pair or not at all: half of one is not a
	// login. Correcting only the registry name is the exception, and it
	// is why the name is worth having here — making someone re-enter a
	// token to fix a typo is how a typo stays.
	var user, pass *string
	switch {
	case username != "" && password != "":
		user, pass = &username, &password
	case username != "" || password != "":
		if username == "" {
			return nil, ErrUsernameRequired
		}
		return nil, ErrPasswordRequired
	case namespace == nil:
		return nil, ErrUsernameRequired
	}

	c, err := s.Repo().Update(ctx, id, user, pass, namespace)
	if errors.Is(err, database.ErrNotFound) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Service) List(ctx context.Context, caller *user.User) ([]*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	return s.Repo().List(ctx)
}

func (s *Service) Delete(ctx context.Context, caller *user.User, id int64) error {
	if err := user.Require(caller, manageRole); err != nil {
		return err
	}
	err := s.Repo().Delete(ctx, id)
	if errors.Is(err, database.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

// ForImage returns the credential this instance holds for whichever
// registry image names, if it holds one.
//
// It runs inside a deploy, on the daemon's behalf rather than a
// caller's, so it does not authorize: by the time a deploy is running,
// the right to deploy that app has already been settled.
func (s *Service) ForImage(ctx context.Context, image string) (*Credential, bool, error) {
	return s.Repo().ByHost(ctx, HostOf(image))
}

// LoginFor is what a pull authenticates with.
//
// For most registries it is what was stored. For AWS it is not: what was
// stored is an access key, and the login is a token fetched from it —
// which is why this exists rather than the deploy path reading Username
// and Password off the row.
func (s *Service) LoginFor(ctx context.Context, c *Credential) (username, password string, err error) {
	if c.Provider != ProviderAWS {
		return c.Username, c.Password, nil
	}

	if cached, ok := s.tokens.Load(c.ID); ok {
		login := cached.(fetchedLogin)
		if time.Now().Add(tokenMargin).Before(login.expires) {
			return login.username, login.password, nil
		}
	}

	auth, err := getECRAuthorization(ctx, s.client, c.Username, c.Password, c.Region)
	if err != nil {
		return "", "", err
	}
	s.tokens.Store(c.ID, fetchedLogin{
		username: auth.Username, password: auth.Password, expires: auth.Expires,
	})
	return auth.Username, auth.Password, nil
}

// ErrNoListing is a registry that cannot say what it holds.
//
// Not every one can. The Docker Registry v2 API defines a catalogue
// endpoint, and the two biggest public registries — Docker Hub and
// GitHub's — disable it. That is their answer, not a failure here, and
// saying so beats an empty list that reads as "you have nothing".
var ErrNoListing = errors.New("this registry does not list what it holds")

// Repositories lists what a credential's registry contains.
func (s *Service) Repositories(ctx context.Context, caller *user.User, id int64) ([]Repo, error) {
	c, err := s.resolve(ctx, caller, id)
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
	c, err := s.resolve(ctx, caller, id)
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
	return listV2Images(ctx, s.client, c, repository)
}

// resolve finds one of this instance's credentials.
func (s *Service) resolve(ctx context.Context, caller *user.User, id int64) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
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
	c, err := s.resolve(ctx, caller, id)
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
	c, err := s.resolve(ctx, caller, id)
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
	c, err := s.resolve(ctx, caller, id)
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
	c, err := s.resolve(ctx, caller, id)
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
	c, err := s.resolve(ctx, caller, id)
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
