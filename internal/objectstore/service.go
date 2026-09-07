package objectstore

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"

	"cubeship/internal/app"
	"cubeship/internal/credential"
	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Index names the service tells one "already exists" from another. They
// are the names the migration gives them.
const (
	slugIndex = "object_stores_slug"
	portIndex = "object_stores_exposed_port"
)

// RoleToManage is the role every write about a *store* takes: creating
// one, linking one, re-pointing it, exposing it, deleting it. An
// admin's, because it starts a container, claims disk nothing reclaims
// on its own, and holds keys to somebody's account.
const RoleToManage = user.RoleAdmin

// RoleForContents is the role every read and write of what is *in* a
// store takes — listing a bucket, downloading a file, uploading one.
//
// An admin's, all of it, and this is the decision worth arguing with.
// The rest of Cubeship lets a member read a great deal: an app's
// configuration, a database's log, what the instance is wired to. What
// it never lets a member read is **data** — there is no way to see a
// row of anybody's database from here either — and a bucket is data.
// What is in one is whatever the apps on this instance put there:
// uploads, dumps, a config file with a key in it. Knowing what you are
// deploying into does not extend to reading it.
//
// A member deploying an app still uses the store, because the app is
// given the keys and the app is what reads the bucket.
const RoleForContents = user.RoleAdmin

// Service holds the object storage use cases.
//
// It sits above credential, which is where an external store's login
// lives, and beside nothing else: no app, project or environment knows
// this module exists, and a store belongs to the instance rather than
// to any of them — the same shape as a datastore, for the same reason.
type Service struct {
	db    *database.DB
	creds *credential.Service
	// apps is what an attachment names. This module sits above that one
	// — a store is attached to apps the way a datastore is — and the
	// one thing that has to travel back down does so as an interface
	// app declares. See app.ObjectStoreVars.
	apps     *app.Service
	prov     *Provisioner
	settings *settings.Service
	// connect opens a client for one store. An interface so the use
	// cases can be tested without a bucket anywhere.
	connect Connector
}

func NewService(db *database.DB, creds *credential.Service, apps *app.Service,
	prov *Provisioner, cfg *settings.Service,
) *Service {
	return &Service{db: db, creds: creds, apps: apps, prov: prov, settings: cfg, connect: S3Connector{}}
}

// SetConnector replaces how this service reaches a store. For tests.
func (s *Service) SetConnector(c Connector) { s.connect = c }

func (s *Service) Repo() *Repository         { return NewRepository(s.db) }
func (s *Service) Provisioner() *Provisioner { return s.prov }
func (s *Service) WaitForProvisioning()      { s.prov.Wait() }

// UsesCredential answers what deleting one credential would break here.
// See credential.Dependant: this module owns these rows, so it is the
// one that can say.
func (s *Service) UsesCredential(ctx context.Context, credentialID int64) ([]credential.Use, error) {
	slugs, err := s.Repo().UsingCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	uses := make([]credential.Use, 0, len(slugs))
	for _, name := range slugs {
		uses = append(uses, credential.Use{Kind: "object store", Name: name})
	}
	return uses, nil
}

// Resolve looks up a store by name and requires minRole of the caller.
func (s *Service) Resolve(ctx context.Context, caller *user.User, name string, minRole user.Role) (*Store, error) {
	if err := user.Require(caller, minRole); err != nil {
		return nil, err
	}
	store, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	// With nothing above a store, what it is wired to is the whole of
	// where it sits in the instance — so it is loaded with it rather
	// than asked for separately.
	if store.Attachments, err = s.Repo().Attachments(ctx, store.ID); err != nil {
		return nil, err
	}
	return store, nil
}

// List is every store on the instance.
//
// A member's, like the list of databases: what storage exists and where
// it is, is part of knowing what you are deploying into. No secret is
// on it — the keys are their own request.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Store, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	// One query for every store's attachments rather than one per row:
	// this is the screen the sidebar opens on.
	byStore, err := s.Repo().AllAttachments(ctx)
	if err != nil {
		return nil, err
	}
	for _, store := range all {
		store.Attachments = byStore[store.ID]
	}
	return all, nil
}

// ManagedSpec is what a MinIO on this instance is created from.
type ManagedSpec struct {
	Slug        string
	Description string
	// Version is a release this Cubeship offers. Empty takes the
	// newest, and either way it is permanent.
	Version string
	// AccessKey and SecretKey are generated when empty, which is the
	// normal answer: a store whose keys somebody typed in a hurry is
	// how storage ends up with a password on the open internet.
	AccessKey string
	SecretKey string
	// Expose asks for a host port at creation. Nil is the normal
	// answer — see Service.Expose.
	Expose *int
}

// Create provisions a MinIO on this instance.
//
// The row is written and the container started detached, so this
// returns as soon as there is something to report on rather than
// holding the request open for an image pull. The store comes back in
// "provisioning"; how it went lands on the same row.
func (s *Service) Create(ctx context.Context, caller *user.User, spec ManagedSpec) (*Store, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, err
	}
	if err := s.checkSlug(spec.Slug); err != nil {
		return nil, err
	}
	if spec.Version == "" {
		spec.Version = DefaultVersion()
	}
	if !KnowsVersion(spec.Version) {
		return nil, fmt.Errorf("%w: %q — this release offers %v", ErrUnknownVersion, spec.Version, Versions())
	}
	if spec.AccessKey == "" || spec.SecretKey == "" {
		accessKey, secretKey, err := GenerateKeys()
		if err != nil {
			return nil, err
		}
		if spec.AccessKey == "" {
			spec.AccessKey = accessKey
		}
		if spec.SecretKey == "" {
			spec.SecretKey = secretKey
		}
	}
	// MinIO refuses to start on either of these, and it refuses by
	// exiting — so without the check the failure is a container that
	// died, minutes after the request, with the reason in a log nobody
	// went looking for.
	if len(spec.AccessKey) < 3 {
		return nil, fmt.Errorf("%w: the access key must be at least 3 characters", ErrBadKey)
	}
	if len(spec.SecretKey) < 8 {
		return nil, fmt.Errorf("%w: the secret key must be at least 8 characters", ErrBadKey)
	}

	port := 0
	if spec.Expose != nil {
		var err error
		if port, err = s.resolvePort(ctx, *spec.Expose); err != nil {
			return nil, err
		}
	}

	created, err := s.Repo().Create(ctx, &Store{
		Slug: spec.Slug, Description: spec.Description,
		Kind: KindManaged, Provider: ProviderMinIO,
		Region: DefaultRegion,
		// Its own endpoint is http on a private Docker network and the
		// bucket goes in the path: MinIO has no wildcard certificate
		// for a container name, and no name to put one on.
		Secure: false, PathStyle: true,
		Version: spec.Version, AccessKey: spec.AccessKey, SecretKey: spec.SecretKey,
		ExposedPort: port, Status: StatusProvisioning,
	})
	if err != nil {
		return nil, createError(err)
	}
	s.prov.Start(created)
	return created, nil
}

// LinkSpec is what an object store somewhere else is linked with.
type LinkSpec struct {
	Slug        string
	Description string
	Provider    Provider
	// Region is AWS's and DigitalOcean's, and the variable part of
	// their endpoint.
	Region string
	// Account is Cloudflare's, and the variable part of R2's endpoint.
	Account string
	// Endpoint is the whole address, for a provider that is not one of
	// the named ones. A URL is accepted: that is what a provider's
	// console shows, and what is on somebody's clipboard.
	Endpoint string
	// Bucket pins this store to exactly one, for a login that reaches
	// one and cannot list them. Empty is the normal case.
	Bucket string
	// CredentialID is the stored account this store authenticates as.
	CredentialID int64
}

// NewLogin is a key pair typed in place of choosing a stored account.
//
// A credential is a **convenience, not a prerequisite** — see
// internal/credential. Somebody linking their first bucket has no
// stored account yet, and being sent to another screen to make one
// before they can do the thing they came to do is the tail wagging the
// dog. So the keys can be typed here, and the account is created from
// them in the same transaction — which means it turns up under
// Credentials afterwards and can be picked for the second bucket, or
// for a registry, or for DNS.
type NewLogin struct {
	// Label is what the stored account is called. Derived from the
	// store when empty: somebody linking a bucket is not necessarily
	// thinking about naming an account.
	Label     string
	AccessKey string
	SecretKey string
}

// Link records an object store this instance does not run.
//
// Nothing is started and nothing is checked against the endpoint here.
// That is deliberate and it is the same call extregistry makes for a
// generic registry: the credential may be scoped to one bucket, the
// endpoint may be behind a network this daemon reaches later, and a
// refusal at this moment would be Cubeship deciding a store is broken
// on evidence it does not have. Whether the login works is answered the
// first time somebody opens it, in the place where the provider's own
// words can be shown.
func (s *Service) Link(ctx context.Context, caller *user.User, spec LinkSpec, login *NewLogin) (*Store, error) {
	if err := user.Require(caller, RoleToManage); err != nil {
		return nil, err
	}
	if err := s.checkSlug(spec.Slug); err != nil {
		return nil, err
	}
	if spec.Provider == ProviderMinIO || !spec.Provider.Valid() {
		// MinIO is what this instance *runs*. One somewhere else is
		// reached as a generic endpoint, because from here that is all
		// it is — and offering it as a choice would suggest linking a
		// MinIO is different from linking anything else.
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, spec.Provider)
	}

	store := &Store{
		Slug: spec.Slug, Description: spec.Description,
		Kind: KindExternal, Provider: spec.Provider,
		Bucket: strings.TrimSpace(spec.Bucket), Status: StatusLinked,
	}
	if store.Bucket != "" {
		if err := CheckBucketName(store.Bucket); err != nil {
			return nil, err
		}
	}
	if err := describeEndpoint(store, spec); err != nil {
		return nil, err
	}

	switch {
	case spec.CredentialID != 0 && login != nil:
		return nil, ErrTwoLogins
	case spec.CredentialID != 0:
		if _, err := s.creds.Resolve(ctx, caller, spec.CredentialID); err != nil {
			return nil, err
		}
		store.CredentialID = spec.CredentialID
		created, err := s.Repo().Create(ctx, store)
		if err != nil {
			return nil, createError(err)
		}
		return created, nil
	case login == nil:
		return nil, ErrCredentialRequired
	}

	// A typed login is two rows and they go in together: an account
	// stored beside a store that turned out to be wrong is a secret
	// nobody asked to keep.
	label := strings.TrimSpace(login.Label)
	if label == "" {
		label = store.Endpoint + " (" + spec.Slug + ")"
	}
	var created *Store
	err := s.db.WithTx(ctx, func(tx database.Queryer) error {
		cred, err := s.creds.CreateWith(ctx, caller, tx, credential.Credential{
			Label: label, Username: login.AccessKey, Password: login.SecretKey,
		})
		if err != nil {
			return err
		}
		store.CredentialID = cred.ID
		created, err = NewRepository(tx).Create(ctx, store)
		return createError(err)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// describeEndpoint fills in where a linked store answers, from the one
// question its provider asks.
//
// Derived rather than typed wherever it can be: an endpoint is a
// template with one variable in it for three of the four providers, and
// asking somebody to retype `s3.eu-central-1.amazonaws.com` is asking
// them to get one character wrong.
func describeEndpoint(store *Store, spec LinkSpec) error {
	region := strings.TrimSpace(spec.Region)
	switch spec.Provider {
	case ProviderAWS:
		if region == "" {
			return ErrRegionRequired
		}
		store.Endpoint = "s3." + region + ".amazonaws.com"
		store.Region = region
		store.Secure = true
		// S3 wants the bucket in the hostname. Path style still works
		// against the regional endpoints, but AWS has said it is going
		// away, and a store linked today should not need re-pointing
		// when it does.
		store.PathStyle = false

	case ProviderDigitalOcean:
		if region == "" {
			return ErrRegionRequired
		}
		store.Endpoint = region + ".digitaloceanspaces.com"
		store.Region = region
		store.Secure = true
		store.PathStyle = false

	case ProviderCloudflare:
		account := strings.TrimSpace(spec.Account)
		if account == "" {
			return ErrAccountRequired
		}
		store.Endpoint = account + ".r2.cloudflarestorage.com"
		// R2 has one region and its name is the literal "auto". A
		// signature computed for anything else is refused.
		store.Region = "auto"
		store.Secure = true
		store.PathStyle = true

	default:
		host, secure, err := endpointHost(spec.Endpoint)
		if err != nil {
			return err
		}
		store.Endpoint = host
		store.Secure = secure
		// Path style, because it is what every S3-compatible server
		// accepts and the virtual-host form needs a wildcard
		// certificate the smaller ones do not have.
		store.PathStyle = true
		store.Region = region
		if store.Region == "" {
			store.Region = DefaultRegion
		}
	}
	return nil
}

func (s *Service) checkSlug(name string) error {
	if slug.Reserved(name) {
		return slug.ErrReserved
	}
	if reservedSlugs[name] {
		return ErrReservedSlug
	}
	if !slug.Valid(name) {
		return slug.ErrInvalid
	}
	return nil
}

// createError turns the unique indexes into the sentence each one
// means. The index is the authority rather than a preceding lookup: two
// concurrent creates would both pass a check, and the loser would
// surface as a 500.
func createError(err error) error {
	switch {
	case err == nil:
		return nil
	case database.UniqueViolationOn(err, portIndex):
		return ErrPortTaken
	case database.UniqueViolationOn(err, slugIndex), database.IsUniqueViolation(err):
		return ErrAlreadyExists
	}
	return err
}

// Update changes a store's description, and which account an external
// one authenticates as.
//
// Not the name, which is the container's for a managed store and the
// identity for both. Not the endpoint or the provider either: an app
// configured against this store would silently start reaching somewhere
// else, which is the same reason a registry's host is fixed. What can
// change is the account — a second AWS key, a rotated token — and that
// is exactly what a credential is for.
func (s *Service) Update(ctx context.Context, caller *user.User, name string, description *string, credentialID *int64) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if credentialID != nil {
		if store.Kind == KindManaged {
			// There is nothing to re-point. A managed store's keys were
			// minted here and are the server's own; pointing it at
			// somebody's AWS account would not change what it accepts.
			return nil, ErrManagedFixed
		}
		if _, err := s.creds.Resolve(ctx, caller, *credentialID); err != nil {
			return nil, err
		}
	}
	if _, err := s.Repo().Update(ctx, store.ID, description, credentialID); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// Credentials is the login for one store, and where to use it.
type Credentials struct {
	AccessKey string
	SecretKey string
	Region    string
	// Endpoint is where an app on this instance reaches it — a
	// container name on the shared network for a managed store, the
	// provider's address for a linked one.
	Endpoint string
	// External is where something off this host reaches a managed
	// store. Present only while it is exposed and the instance has a
	// domain to be reached at.
	External string
	// PathStyle is worth reporting, because half the S3 clients in the
	// world need telling and the other half guess wrong.
	PathStyle bool
}

// Credentials reads the keys. An admin's, and its own request rather
// than a field on the store: everything else about one is worth listing
// on a screen, and this is worth asking for.
func (s *Service) Credentials(ctx context.Context, caller *user.User, name string) (Credentials, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return Credentials{}, err
	}
	region := store.Region
	if region == "" {
		region = DefaultRegion
	}
	return Credentials{
		AccessKey: store.AccessKey, SecretKey: store.SecretKey,
		Region: region, Endpoint: store.URL(),
		External:  store.ExternalURL(s.externalHost(ctx)),
		PathStyle: store.PathStyle,
	}, nil
}

// externalHost is the name somebody off this host connects to, which is
// the instance's own domain. Empty before there is one: an install
// reached by IP has no name to put in an endpoint, and guessing the
// interface's address would be wrong on every host behind NAT.
func (s *Service) externalHost(ctx context.Context) string {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return values.Get(settings.Domain)
}

// Expose publishes a managed store on a host port, so something that is
// not an app on this instance can reach it: `aws s3 cp` from a laptop,
// a backup script on another machine, a tool that wants an S3 endpoint.
//
// port 0 means "pick one" — see PortRangeStart.
//
// It is off by default and worth leaving off. **There is no TLS in
// front of this**: MinIO answers plain HTTP, Traefik is not in the path,
// and an exposed store is therefore a signed request over the open
// internet — the signature protects the credentials, and nothing
// protects what is being uploaded. What makes it safe is a firewall
// rule, which is yours to write, and which this instance now has a
// screen for.
//
// The container is replaced to pick the port up, because a container's
// published ports are fixed when it is created. The objects survive
// that untouched: they are a host bind mount, the same property
// everything else here relies on when a container's configuration
// changes.
func (s *Service) Expose(ctx context.Context, caller *user.User, name string, port int) (*Store, error) {
	store, err := s.managed(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	chosen, err := s.resolvePort(ctx, port)
	if err != nil {
		return nil, err
	}
	if chosen == store.ExposedPort {
		return store, nil
	}
	return s.setPort(ctx, caller, store, chosen)
}

// Unexpose takes a managed store off its host port, leaving it
// reachable only by its neighbours on the shared network.
func (s *Service) Unexpose(ctx context.Context, caller *user.User, name string) (*Store, error) {
	store, err := s.managed(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	if store.ExposedPort == 0 {
		return store, nil
	}
	return s.setPort(ctx, caller, store, 0)
}

func (s *Service) setPort(ctx context.Context, caller *user.User, store *Store, port int) (*Store, error) {
	if err := s.Repo().SetExposedPort(ctx, store.ID, port); err != nil {
		if database.UniqueViolationOn(err, portIndex) {
			return nil, ErrPortTaken
		}
		return nil, err
	}
	updated, err := s.Resolve(ctx, caller, store.Slug, RoleToManage)
	if err != nil {
		return nil, err
	}
	// Back to provisioning, and detached: the container is replaced,
	// which is a pull-free create-and-start but still not something to
	// hold a request open for. The row is where the outcome goes.
	if err := s.Repo().UpdateContainer(ctx, updated.ID, updated.ContainerID, StatusProvisioning, ""); err != nil {
		return nil, err
	}
	updated.Status = StatusProvisioning
	s.prov.Start(updated)
	return updated, nil
}

// resolvePort turns what a caller asked for into a port to publish on.
// 0 means "pick one".
func (s *Service) resolvePort(ctx context.Context, want int) (int, error) {
	used, err := s.Repo().UsedPorts(ctx)
	if err != nil {
		return 0, err
	}
	if want != 0 {
		// Below 1024 needs privileges this container does not have.
		// Beyond that Docker is the authority — a port held by
		// something Cubeship did not start fails at bind time, and no
		// list here could have known about it.
		if want < 1024 || want > 65535 {
			return 0, ErrBadPort
		}
		if used[want] {
			return 0, ErrPortTaken
		}
		return want, nil
	}
	for port := PortRangeStart; port <= PortRangeEnd; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, ErrNoPortsLeft
}

// Stop turns a managed store off, leaving its container and its objects
// where they are.
func (s *Service) Stop(ctx context.Context, caller *user.User, name string) (*Store, error) {
	store, err := s.managed(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	if store.ContainerID == "" {
		return nil, ErrNotRunning
	}
	if err := s.prov.Stop(ctx, store); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// Start brings a stopped store back.
//
// It provisions rather than starting the container that is already
// there — one path instead of two. The container is recreated from the
// same options and the objects are a host bind mount a recreate does
// not touch. It is also how a store whose provisioning failed is
// retried.
func (s *Service) Start(ctx context.Context, caller *user.User, name string) (*Store, error) {
	store, err := s.managed(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	if err := s.Repo().UpdateContainer(ctx, store.ID, store.ContainerID, StatusProvisioning, ""); err != nil {
		return nil, err
	}
	store.Status = StatusProvisioning
	s.prov.Start(store)
	return store, nil
}

// DefaultLogTail is how much of a managed store's log the API returns
// when the caller does not ask for an amount.
const DefaultLogTail = "500"

// Logs is what the server has written. A member's, like a database's:
// the reason a store is refusing is in its log, and finding that out is
// not an admin's privilege. It carries no key — MinIO prints its own
// startup, not what it was configured with.
func (s *Service) Logs(ctx context.Context, caller *user.User, name, tail string) (io.ReadCloser, error) {
	store, err := s.Resolve(ctx, caller, name, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if store.Kind != KindManaged {
		return nil, ErrExternalHasNoContainer
	}
	if store.ContainerID == "" {
		return nil, ErrNotRunning
	}
	if tail == "" {
		tail = DefaultLogTail
	}
	return s.prov.Logs(ctx, store, tail)
}

// Delete removes a store.
//
// For a managed one that is its container and its objects, and the
// objects go: storage whose files outlived it would leave a directory
// nothing on the instance names any more and nothing ever reclaims —
// and on a box this size that is the largest thing on the disk. The
// guard is the confirmation in front of it, which asks for the store's
// own name.
//
// For a linked one **nothing anywhere else is touched**. This instance
// forgets an endpoint and a login; the bucket and everything in it stay
// exactly where they are. That asymmetry is the whole difference
// between the two kinds and is worth saying wherever this is offered.
func (s *Service) Delete(ctx context.Context, caller *user.User, name string) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if store.Kind == KindManaged {
		// Container and files first, outside any transaction, because
		// neither Docker nor the filesystem has a rollback. A failure
		// there leaves the row standing, which a retry finishes; the
		// reverse would leave a server running with nothing on the
		// instance naming it.
		if err := s.prov.Teardown(ctx, store, false); err != nil {
			return nil, err
		}
	}
	if err := s.Repo().Delete(ctx, store.ID); err != nil {
		return nil, err
	}
	return store, nil
}

// managed resolves a store and refuses one this instance does not run.
func (s *Service) managed(ctx context.Context, caller *user.User, name string) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if store.Kind != KindManaged {
		return nil, ErrExternalHasNoContainer
	}
	return store, nil
}

// -- what is inside a store ------------------------------------------

// client resolves a store, requires the contents role, and opens a
// connection to it. Every call below goes through it, so the
// authorization question is asked once and in one place.
func (s *Service) client(ctx context.Context, caller *user.User, name string) (*Store, Client, error) {
	store, err := s.Resolve(ctx, caller, name, RoleForContents)
	if err != nil {
		return nil, nil, err
	}
	c, err := s.connect.Connect(ctx, store)
	if err != nil {
		return nil, nil, err
	}
	return store, c, nil
}

// Buckets is what this store holds.
//
// A store pinned to one bucket answers with that one and asks the
// endpoint nothing: its login reaches a single bucket and listing them
// is exactly what it is not allowed to do, so the call would fail for a
// question this instance already knows the answer to.
func (s *Service) Buckets(ctx context.Context, caller *user.User, name string) ([]Bucket, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return nil, err
	}
	if store.Bucket != "" {
		return []Bucket{{Name: store.Bucket}}, nil
	}
	return c.Buckets(ctx)
}

// CreateBucket makes one.
func (s *Service) CreateBucket(ctx context.Context, caller *user.User, name, bucket string) error {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return err
	}
	bucket = strings.TrimSpace(bucket)
	if err := CheckBucketName(bucket); err != nil {
		return err
	}
	if store.Bucket != "" && store.Bucket != bucket {
		return ErrSingleBucket
	}
	return c.MakeBucket(ctx, bucket)
}

// DeleteBucket removes an empty one.
//
// Empty, because S3 refuses otherwise and that refusal is worth
// keeping: a bucket delete that quietly took a thousand objects with it
// is the one mistake here nobody recovers from. Emptying it is a
// separate act, one folder at a time, each with its own confirmation.
func (s *Service) DeleteBucket(ctx context.Context, caller *user.User, name, bucket string) error {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return err
	}
	return c.RemoveBucket(ctx, bucket)
}

// Browse is one level of one bucket: the folders directly under a
// prefix and the objects directly in it.
func (s *Service) Browse(ctx context.Context, caller *user.User, name, bucket, prefix, cursor string, limit int) (Listing, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return Listing{}, err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return Listing{}, err
	}
	clean, err := CleanPrefix(prefix)
	if err != nil {
		return Listing{}, err
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	return c.List(ctx, bucket, clean, cursor, limit)
}

// Upload writes one object. size may be -1 for a stream whose length is
// not known, which is what an upload from a browser is.
func (s *Service) Upload(ctx context.Context, caller *user.User, name, bucket, prefix, filename string, r io.Reader, size int64, contentType string) (string, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return "", err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return "", err
	}
	clean, err := CleanPrefix(prefix)
	if err != nil {
		return "", err
	}
	key, err := CleanKey(clean, filename)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(key, "/") {
		// A folder is not a file, and writing one here would put an
		// object where the listing expects a prefix.
		return "", ErrBadKey
	}
	return key, c.Put(ctx, bucket, key, r, size, contentType)
}

// CreateFolder writes the zero-byte object that makes an empty folder
// appear in a listing.
//
// There is no other way. S3 has no directories — a listing's folders
// are common prefixes, which exist only while something is under them —
// so an empty folder is a convention, and this is the one every console
// uses.
func (s *Service) CreateFolder(ctx context.Context, caller *user.User, name, bucket, prefix, folder string) (string, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return "", err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return "", err
	}
	clean, err := CleanPrefix(prefix)
	if err != nil {
		return "", err
	}
	key, err := CleanKey(clean, strings.TrimSuffix(strings.TrimSpace(folder), "/"))
	if err != nil {
		return "", err
	}
	key += FolderMarker
	return key, c.Put(ctx, bucket, key, strings.NewReader(""), 0, "application/x-directory")
}

// Download reads one object out, for streaming to whoever asked.
//
// Streamed through the daemon rather than handed over as a presigned
// URL, and that is not a shortcut. A managed store's endpoint is a
// container name that resolves on the Docker network and nowhere else,
// so a presigned link would be one only the daemon could follow — and
// two code paths where one of them is exercised on external stores only
// is one that breaks quietly.
func (s *Service) Download(ctx context.Context, caller *user.User, name, bucket, key string) (io.ReadCloser, Object, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return nil, Object{}, err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return nil, Object{}, err
	}
	if key == "" || strings.HasSuffix(key, "/") {
		return nil, Object{}, ErrBadKey
	}
	return c.Get(ctx, bucket, key)
}

// DeleteObject removes one file.
func (s *Service) DeleteObject(ctx context.Context, caller *user.User, name, bucket, key string) error {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return err
	}
	if key == "" {
		return ErrBadKey
	}
	return c.Remove(ctx, bucket, key)
}

// DeleteFolder removes everything under a prefix, and answers how many
// objects went.
//
// Everything under it, at every depth, because that is what a folder
// is: there is nothing else to delete. The count comes back so the
// answer can say what actually happened — "deleted" over a prefix that
// held four hundred files is worth being told about after the fact.
func (s *Service) DeleteFolder(ctx context.Context, caller *user.User, name, bucket, prefix string) (int, error) {
	store, c, err := s.client(ctx, caller, name)
	if err != nil {
		return 0, err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return 0, err
	}
	clean, err := CleanPrefix(prefix)
	if err != nil {
		return 0, err
	}
	if clean == "" {
		// The bucket's root is not a folder. Emptying a whole bucket by
		// passing an empty prefix is the kind of thing that happens
		// when a query parameter goes missing, so it is refused rather
		// than obeyed.
		return 0, ErrBadKey
	}
	return c.RemoveAll(ctx, bucket, clean)
}

// checkBucket refuses reaching past the one bucket a store was pinned
// to. Without it, a store linked with a scoped credential would still
// let somebody type another bucket's name into the URL — the endpoint
// would refuse, but with an access-denied nobody can act on rather than
// the sentence that explains it.
func (s *Service) checkBucket(store *Store, bucket string) error {
	if bucket == "" {
		return ErrBadBucket
	}
	if store.Bucket != "" && store.Bucket != bucket {
		return ErrSingleBucket
	}
	return nil
}

// ExternalHost is the instance's own domain, which is where an exposed
// store is reached from off this host. Exported for the handlers, which
// read it once per response rather than once per row.
func (s *Service) ExternalHost(ctx context.Context) string { return s.externalHost(ctx) }

// -- attachments -----------------------------------------------------

// Attach wires an app to one bucket in this store: the app's container
// is given S3_ENDPOINT and its parts from its next deploy onwards.
//
// From its next deploy, not now. A container keeps the environment it
// was created with — the same rule that makes adding a domain take
// effect on redeploy — so attaching something an app is already running
// against changes nothing until it is deployed again.
//
// appRef is the app's full reference, project/environment/name. It has
// to be: a store is not inside an environment, so a bare name would
// identify nothing, and one bucket serving apps in two projects is the
// reason this module is instance-wide at all.
//
// **The bucket is not checked against the store.** Whether it exists is
// a live call this instance may not be allowed to make — a credential
// scoped to one bucket may not stat another — and refusing on evidence
// it does not have is how a working attachment gets blocked. The screen
// offers the buckets it can list; the API takes the name.
func (s *Service) Attach(ctx context.Context, caller *user.User, name, appRef, bucket, prefix string) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	bucket = strings.TrimSpace(bucket)
	if err := CheckBucketName(bucket); err != nil {
		return nil, err
	}
	if err := s.checkBucket(store, bucket); err != nil {
		return nil, err
	}
	if err := CheckPrefix(prefix); err != nil {
		return nil, err
	}
	// The app is resolved at the same role, which is what makes this an
	// admin's act from both ends: it hands an app the store's keys.
	a, err := s.apps.ResolveString(ctx, caller, appRef, RoleToManage)
	if err != nil {
		return nil, err
	}

	if err := s.Repo().Attach(ctx, store.ID, a.ID, bucket, prefix); err != nil {
		switch {
		case database.UniqueViolationOn(err, "object_store_attachments_pair"):
			return nil, ErrAlreadyAttached
		case database.UniqueViolationOn(err, "object_store_attachments_app_vars"):
			return nil, PrefixTakenError(prefix)
		case database.IsUniqueViolation(err):
			return nil, ErrAlreadyAttached
		}
		return nil, fmt.Errorf("attach object store: %w", err)
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// Detach unwires an app from one bucket. Its container keeps the
// variables it was created with until it is deployed again — which is
// worth knowing, because detaching is not how you cut an app off from a
// bucket in a hurry. Rotating the credential is.
func (s *Service) Detach(ctx context.Context, caller *user.User, name, appRef, bucket string) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	a, err := s.apps.ResolveString(ctx, caller, appRef, RoleToManage)
	if err != nil {
		return nil, err
	}
	removed, err := s.Repo().Detach(ctx, store.ID, a.ID, bucket)
	if err != nil {
		return nil, err
	}
	if !removed {
		return nil, ErrNotAttached
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// VarsForApp is what app asks this module for: the variables every
// bucket attached to one app contributes to its container.
//
// Read fresh at every deploy rather than stored on the app, so an
// attachment made after the last deploy is picked up — and so a rotated
// key reaches the app on its next deploy without anybody editing
// anything.
func (s *Service) VarsForApp(ctx context.Context, appID int64) (envvar.Map, error) {
	attached, err := s.Repo().AttachedTo(ctx, appID)
	if err != nil {
		return nil, err
	}
	vars := envvar.Map{}
	for i := range attached {
		a := &attached[i]
		maps.Copy(vars, a.Store.Vars(a.Prefix, a.Bucket))
	}
	return vars, nil
}
