package objectstore

import (
	"context"
	"io"
	"strings"

	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

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
	s.syncPorts(ctx)
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
	store, err := s.Resolve(ctx, caller, name, user.LevelView)
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
	// A linked store is never exposed; the kind is checked anyway, so a
	// stray number on one cannot reach the host.
	if store.Kind == KindManaged && store.ExposedPort != 0 {
		s.syncPorts(ctx)
	}
	// The series outlives nothing: the id is free to be reused by the
	// next store, and a chart that opened on somebody else's history
	// would be worse than an empty one.
	return store, s.metrics.Forget(ctx, metrics.KindObjectStore, store.ID)
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
func (s *Service) client(ctx context.Context, caller *user.User, name string, write bool) (*Store, Client, error) {
	store, err := s.Resolve(ctx, caller, name, user.LevelView)
	if err != nil {
		return nil, nil, err
	}
	// What is in a bucket is data somebody put there: reading it is a
	// secret, changing it is managing the store.
	if write {
		err = user.Allow(caller, user.ResStorage, user.LevelManage, name)
	} else {
		err = user.AllowSecrets(caller, user.ResStorage, name)
	}
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
	store, c, err := s.client(ctx, caller, name, false)
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
	store, c, err := s.client(ctx, caller, name, true)
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
	store, c, err := s.client(ctx, caller, name, true)
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
	store, c, err := s.client(ctx, caller, name, false)
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
	store, c, err := s.client(ctx, caller, name, true)
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
