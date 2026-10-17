package objectstore

import (
	"context"
	"errors"
	"fmt"
	"log"

	"cubeship/internal/app"
	"cubeship/internal/credential"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
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
const RoleToManage = user.LevelManage

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
	// metrics answers what a managed store's container has been using.
	// The same package an app and a database go through: MinIO is a
	// container with a CPU and a resident set like any other.
	metrics *metrics.Service
	// portsChanged is told after anything changes what this module
	// publishes on the host. Nil in tests and wherever nothing is set.
	portsChanged PortsChanged
}

// PortsChanged is told whenever what this module publishes on the host changes.
type PortsChanged interface {
	SyncPublished(ctx context.Context) error
}

// SetPortsChanged names what is told when an exposed port comes or goes.
// A setter because what satisfies it is built after this module.
func (s *Service) SetPortsChanged(p PortsChanged) { s.portsChanged = p }

// syncPorts tells it, best effort: the expose has happened either way,
// and refusing the request would not undo it.
func (s *Service) syncPorts(ctx context.Context) {
	if s.portsChanged == nil {
		return
	}
	if err := s.portsChanged.SyncPublished(ctx); err != nil {
		log.Printf("object store: could not bring the host firewall in line with what is exposed: %v", err)
	}
}

func NewService(db *database.DB, creds *credential.Service, apps *app.Service,
	prov *Provisioner, cfg *settings.Service, series *metrics.Service,
) *Service {
	return &Service{db: db, creds: creds, apps: apps, prov: prov, settings: cfg,
		connect: S3Connector{}, metrics: series}
}

// Metrics exposes the series service to this module's own handlers.
func (s *Service) Metrics() *metrics.Service { return s.metrics }

// MetricSubjects is what the collector samples on this module's behalf:
// every store with a container behind it right now.
//
// Which is only ever a managed one. A linked store is somebody else's
// server — there is nothing here to read a cgroup from, and its row
// carries no container id, so it falls out of this on the same test a
// stopped MinIO does.
func (s *Service) MetricSubjects(ctx context.Context) ([]metrics.Subject, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]metrics.Subject, 0, len(all))
	for _, store := range all {
		if store.ContainerID == "" {
			continue
		}
		out = append(out, metrics.Subject{
			Kind: metrics.KindObjectStore, ID: store.ID, ContainerID: store.ContainerID, Name: store.Slug,
		})
	}
	return out, nil
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
func (s *Service) Resolve(ctx context.Context, caller *user.User, name string, need user.Level) (*Store, error) {
	if err := user.Allow(caller, user.ResStorage, need, name); err != nil {
		if errors.Is(err, user.ErrHidden) {
			return nil, ErrNotFound
		}
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
	seen := all[:0]
	for _, store := range all {
		if !user.Sees(caller, user.ResStorage, store.Slug) {
			continue
		}
		seen = append(seen, store)
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
	if err := user.Allow(caller, user.ResStorage, RoleToManage, spec.Slug); err != nil {
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
	if created.ExposedPort != 0 {
		s.syncPorts(ctx)
	}
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
