package objectstore

import (
	"context"
	"fmt"
	"strings"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

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
	if err := user.Allow(caller, user.ResStorage, RoleToManage, spec.Slug); err != nil {
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
		// Accepted whatever the provider is. It was refused for a
		// while everywhere but R2 and Spaces, on the grounds that those
		// are the ones whose logins are routinely issued per bucket —
		// and that was one step too far. An IAM policy can be narrowed
		// to a single bucket and a compatible endpoint can be anything
		// at all, so a login that reaches exactly one is a thing
		// somebody can have on any provider here. What the field costs
		// is worth *saying*, which the form does, rather than refusing
		// on a guess about somebody else's credential.
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
		if !ValidHostLabel(region) {
			return ErrInvalidRegion
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
		if !ValidHostLabel(region) {
			return ErrInvalidRegion
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
		if !ValidHostLabel(account) {
			return ErrInvalidAccount
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
// Update changes what a store is allowed to change about itself: its
// description, which stored account it authenticates as, its ceiling,
// and which bucket it is pinned to — including to none.
//
// `bucket` is a pointer because empty is a value here rather than a gap:
// it is how a store is **unpinned**, which is the direction that can
// never be wrong. Pinning narrows the store and is refused where the
// provider's logins are not issued that way; unpinning only hands the
// question back to the endpoint, so it is accepted whatever the
// provider is — which matters for a store pinned before that rule
// existed and has no other way out.
func (s *Service) Update(ctx context.Context, caller *user.User, name string, description *string, credentialID *int64, bucket *string, l *Limits) (*Store, error) {
	store, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if bucket != nil {
		trimmed := strings.TrimSpace(*bucket)
		bucket = &trimmed
		if err := s.checkRepin(ctx, store, trimmed); err != nil {
			return nil, err
		}
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
	if l != nil {
		if store.Kind != KindManaged {
			// Nothing here to cap. Refused rather than stored and
			// ignored, which is ErrManagedFixed's argument from the
			// other side: each kind refuses the setting that belongs
			// to the other, instead of showing a number that does
			// nothing.
			return nil, ErrLinkedHasNoContainer
		}
		if !l.Valid() {
			return nil, ErrInvalidLimits
		}
	}
	updated, err := s.Repo().Update(ctx, store.ID, description, credentialID, bucket, l)
	if err != nil {
		return nil, err
	}
	// A ceiling is the one part of the container that changes without
	// the container being replaced, unlike publishing a port.
	if l != nil {
		if err := s.prov.Cap(ctx, updated); err != nil {
			return nil, err
		}
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
	store, err := s.Resolve(ctx, caller, name, user.LevelView)
	if err != nil {
		return Credentials{}, err
	}
	if err := user.AllowSecrets(caller, user.ResStorage, name); err != nil {
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
	// Asked for the port it already holds: nothing to change, and the
	// port is in use — by this one — so resolvePort would refuse it.
	if port != 0 && port == store.ExposedPort {
		return store, nil
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
