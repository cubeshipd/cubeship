package extregistry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// Service holds the use cases for registry credentials. Both the HTTP
// handlers and the deploy path call exactly these.
type Service struct {
	db *database.DB

	// creds is where the secrets live now. A registry row says which
	// account it authenticates as; this is what turns that into a
	// login, and what lets a login typed in place of a stored one
	// become a credential in the same transaction.
	creds *credential.Service

	// client talks to a provider's API — AWS's, so far. Only a
	// credential whose token has to be fetched needs one.
	client *http.Client
	// tokens holds fetched registry logins until shortly before they
	// expire. An ECR token lasts twelve hours and a pull takes seconds
	// of that; fetching one per pull would be a signed round trip
	// against a rate limit for nothing.
	tokens sync.Map // credential id -> fetchedLogin
}

func NewService(db *database.DB, creds *credential.Service) *Service {
	return &Service{
		db:     db,
		creds:  creds,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// UsesCredential answers what a delete of one credential would break
// here. See credential.Dependant — this module owns these rows, so it
// is the one that can say.
func (s *Service) UsesCredential(ctx context.Context, credentialID int64) ([]credential.Use, error) {
	hosts, err := s.Repo().UsingCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	uses := make([]credential.Use, 0, len(hosts))
	for _, host := range hosts {
		uses = append(uses, credential.Use{Kind: "registry", Name: host})
	}
	return uses, nil
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
// The login comes from one of two places, and neither is privileged
// over the other: a stored account, or one typed here — see NewLogin.
func (s *Service) Create(ctx context.Context, caller *user.User, in Credential, login *NewLogin) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	switch {
	case in.CredentialID != 0 && login != nil:
		// Both is not a request with an obvious reading, and guessing
		// which one was meant is how the wrong secret gets stored.
		return nil, ErrTwoLogins

	case in.CredentialID != 0:
		// The account supplies the login used below to prove the key
		// works. Which provider this is comes from the request: a
		// credential carries none, because the same access key may be
		// writing DNS records with the other hand.
		cred, err := s.creds.Resolve(ctx, caller, in.CredentialID)
		if err != nil {
			return nil, err
		}
		in.Username, in.Password = cred.Username, cred.Password

	case login != nil:
		in.Username, in.Password = login.Username, login.Password

	default:
		return nil, ErrCredentialRequired
	}
	if !in.Provider.Valid() {
		return nil, ErrUnknownProvider
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
		auth, ecrErr := getECRAuthorization(ctx, s.client, in.Username, in.Password, in.Region)
		if ecrErr != nil {
			return nil, ecrErr
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

	if login == nil {
		c, err := s.Repo().Create(ctx, in)
		if database.IsUniqueViolation(err) {
			return nil, ErrHostTaken
		}
		return c, err
	}

	// A typed login is two rows, and they go in together: an account
	// stored beside a registry that turned out to be unreachable is a
	// secret nobody asked to keep.
	//
	// The label is derived when none was given, from the host — which
	// is unique here, so the derived one is too. Somebody adding a
	// registry is thinking about the registry, not about naming an
	// account they did not know they were creating.
	label := login.Label
	if strings.TrimSpace(label) == "" {
		label = in.Host
		if in.Namespace != "" {
			label += "/" + in.Namespace
		}
	}

	var created *Credential
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		cred, err := s.creds.CreateWith(ctx, caller, tx, credential.Credential{
			Label: label,
			// What is stored is what was typed. The DigitalOcean
			// doubling above is how the login is *used*, not what the
			// account is — the token has no name beside it.
			Username: login.Username,
			Password: login.Password,
		})
		if err != nil {
			return err
		}
		in.CredentialID = cred.ID
		created, err = NewRepository(tx).Create(ctx, in)
		if database.IsUniqueViolation(err) {
			return ErrHostTaken
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Changes is what may be edited about a registry.
//
// Every field is a pointer because "leave it alone" and "set it to
// this" are different requests, and a plain value cannot tell them
// apart.
type Changes struct {
	// CredentialID re-points the registry at a different stored
	// account. Any of them: a credential is a secret, not a kind of
	// registry, and whether it works is the registry's to refuse.
	CredentialID *int64
	// Namespace corrects DigitalOcean's registry name.
	Namespace *string
	// Username and Password rotate the login **on the account this
	// registry authenticates as**, which is the point of the account:
	// every registry on it follows the one edit. See Update.
	Username *string
	Password *string
}

func (c Changes) empty() bool {
	return c.CredentialID == nil && c.Namespace == nil &&
		c.Username == nil && c.Password == nil
}

// Update re-points a registry at a different account, rotates the login
// of the account it has, and corrects a namespace typed by hand.
//
// The host is not editable — an app's pulls are matched to a registry by
// host, so re-pointing one in place would silently send them somewhere
// else.
//
// **Rotating from here rotates the account**, not a copy of it: that is
// what a shared secret means, and it is the improvement — the same token
// used to be re-entered once per registry that held it. A caller that
// wanted only this registry to move wanted a different account, which is
// what CredentialID is for. Asking for both at once has no obvious
// reading — it would rotate the account being pointed at, which is
// nobody's intent — so it is refused.
func (s *Service) Update(ctx context.Context, caller *user.User, id int64, ch Changes) (*Credential, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if ch.empty() {
		return nil, ErrNothingToUpdate
	}
	rotating := ch.Username != nil || ch.Password != nil
	if ch.CredentialID != nil && rotating {
		return nil, ErrTwoLogins
	}

	existing, err := s.resolve(ctx, caller, id)
	if err != nil {
		return nil, err
	}

	if ch.CredentialID != nil {
		if _, err := s.creds.Resolve(ctx, caller, *ch.CredentialID); err != nil {
			return nil, err
		}
	}

	if rotating {
		if _, err := s.creds.Update(ctx, caller,
			existing.CredentialID, nil, ch.Username, ch.Password); err != nil {
			return nil, err
		}
		// A cached ECR token was minted from the key that just
		// changed. Keeping it would leave pulls working on the old key
		// for hours and then failing for no visible reason.
		s.tokens.Delete(existing.CredentialID)
	}

	if ch.Namespace != nil {
		normalized, err := checkNamespace(ctx, s, caller, id, *ch.Namespace)
		if err != nil {
			return nil, err
		}
		ch.Namespace = &normalized
	}

	if ch.CredentialID == nil && ch.Namespace == nil {
		// Only the login changed, and that is a row in another table.
		// Read the registry back so the caller sees the new username.
		return s.resolve(ctx, caller, id)
	}

	c, err := s.Repo().Update(ctx, id, ch.CredentialID, ch.Namespace)
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
		// Not the stored values: DigitalOcean's token is both halves of
		// the login and the account has no username. See Credential.Login.
		username, password := c.Login()
		return username, password, nil
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
