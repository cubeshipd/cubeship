package objectstore

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

// CreateFolder writes the zero-byte object that makes an empty folder
// appear in a listing.
//
// There is no other way. S3 has no directories — a listing's folders
// are common prefixes, which exist only while something is under them —
// so an empty folder is a convention, and this is the one every console
// uses.
func (s *Service) CreateFolder(ctx context.Context, caller *user.User, name, bucket, prefix, folder string) (string, error) {
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
	store, c, err := s.client(ctx, caller, name, false)
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
	store, c, err := s.client(ctx, caller, name, true)
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
	store, c, err := s.client(ctx, caller, name, true)
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

// checkRepin decides whether a store may be pinned to `bucket`, or
// unpinned when it is empty.
//
// Two refusals, and the second is the one worth reading. A managed
// store is this instance's own MinIO and lists its own buckets, so
// pinning it is a limit invented out of nothing. And a bucket that is
// not the one an attached app is pointed at would leave the store
// claiming to be one bucket while an attachment names another — the app
// keeps working, because its keys reach the endpoint directly, and the
// dashboard stops being able to show the bucket it is using. Refused
// with the names, the way deleting a credential something stands on is.
//
// The provider is not one of them. Any login here may have been issued
// for a single bucket, so pinning is accepted wherever linking is.
func (s *Service) checkRepin(ctx context.Context, store *Store, bucket string) error {
	if bucket == "" {
		return nil
	}
	if store.Kind == KindManaged {
		return ErrManagedFixed
	}
	if err := CheckBucketName(bucket); err != nil {
		return err
	}
	attached, err := s.Repo().Attachments(ctx, store.ID)
	if err != nil {
		return err
	}
	var elsewhere []string
	for _, a := range attached {
		if a.Bucket != bucket {
			elsewhere = append(elsewhere, a.AppRef+" \u2192 "+a.Bucket)
		}
	}
	if len(elsewhere) > 0 {
		return fmt.Errorf("%w: %s", ErrAttachedElsewhere, strings.Join(elsewhere, ", "))
	}
	return nil
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

// --- what backups need, and nothing more ---

// ClientForID opens a client for one store by its id, with no caller.
//
// **It authorizes nothing, deliberately**, and that is safe because of
// who calls it: `internal/backup`, which has already settled whether
// somebody may back this database up before it gets here — and the
// scheduler, where there is no caller to ask about at all. The same
// shape `metrics.Service` has: asking twice is two answers to a
// question with one.
//
// By id rather than by slug because a backup names where it went with a
// foreign key. A slug is what somebody types; an id is what survives
// them renaming nothing, since a store's slug never changes anyway —
// what it survives is this instance holding two answers to which store
// a five-month-old dump is in.
// IDForName is how a module that stores a foreign key gets one from
// what a person typed. Every surface in this product addresses a store
// by its name; a row that points at one holds its id, because a slug
// somebody could have reused is not a key.
//
// Caller-free for the reason ClientForID is.
func (s *Service) IDForName(ctx context.Context, name string) (int64, error) {
	store, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return 0, ErrNotFound
	}
	return store.ID, nil
}

// NameForID is the other direction, for a listing that has the key and
// wants what to print.
func (s *Service) NameForID(ctx context.Context, id int64) string {
	store, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return ""
	}
	return store.Slug
}

// LeavesThisMachine reports whether writing to this store puts the
// bytes anywhere but the disk the daemon is running on.
//
// **A managed store does not.** It is a MinIO container this instance
// runs, and its objects are a bind mount under the data directory — the
// same disk as everything else here. Callers that care about durability
// rather than about tidiness have to tell the two apart, and "it went
// to an object store" is not the same question.
//
// A store that is gone answers false: whatever it held is unreachable,
// which is not somewhere safe by any reading.
func (s *Service) LeavesThisMachine(ctx context.Context, id int64) bool {
	store, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return false
	}
	return store.Kind == KindExternal
}

func (s *Service) ClientForID(ctx context.Context, id int64) (*Store, Client, error) {
	store, err := s.Repo().ByID(ctx, id)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	c, err := s.connect.Connect(ctx, store)
	if err != nil {
		return nil, nil, err
	}
	return store, c, nil
}
