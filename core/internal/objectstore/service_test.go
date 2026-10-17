package objectstore_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/objectstore"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// fakeStore is an S3 endpoint in a map.
//
// The whole point of the Connector seam: everything worth testing here
// is this instance's own — who may look, which bucket a request may
// reach, what a folder is — and none of it is worth a container.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]map[string][]byte // bucket -> key -> content
	// denied makes every call answer the way a store refuses a login
	// this instance holds, which is a different thing from Cubeship
	// refusing the caller.
	denied bool
}

func newFakeStore(buckets ...string) *fakeStore {
	f := &fakeStore{objects: map[string]map[string][]byte{}}
	for _, b := range buckets {
		f.objects[b] = map[string][]byte{}
	}
	return f
}

func (f *fakeStore) Connect(context.Context, *objectstore.Store) (objectstore.Client, error) {
	return f, nil
}

func (f *fakeStore) Buckets(context.Context) ([]objectstore.Bucket, error) {
	if f.denied {
		return nil, objectstore.ErrDenied
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []objectstore.Bucket
	for name := range f.objects {
		out = append(out, objectstore.Bucket{Name: name, CreatedAt: time.Unix(0, 0).UTC()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) MakeBucket(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[name]; ok {
		return objectstore.ErrBucketNotEmpty
	}
	f.objects[name] = map[string][]byte{}
	return nil
}

func (f *fakeStore) RemoveBucket(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys, ok := f.objects[name]
	if !ok {
		return objectstore.ErrBucketNotFound
	}
	if len(keys) > 0 {
		return objectstore.ErrBucketNotEmpty
	}
	delete(f.objects, name)
	return nil
}

func (f *fakeStore) List(_ context.Context, bucket, prefix, _ string, _ int) (objectstore.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys, ok := f.objects[bucket]
	if !ok {
		return objectstore.Listing{}, objectstore.ErrBucketNotFound
	}
	out := objectstore.Listing{Prefix: prefix}
	folders := map[string]bool{}
	for key, content := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := key[len(prefix):]
		if rest == "" {
			// The marker for the folder being listed. It is this
			// folder, not something in it.
			continue
		}
		if i := strings.Index(rest, "/"); i >= 0 {
			folders[prefix+rest[:i+1]] = true
			continue
		}
		out.Objects = append(out.Objects, objectstore.Object{Key: key, Size: int64(len(content))})
	}
	for folder := range folders {
		out.Folders = append(out.Folders, folder)
	}
	sort.Strings(out.Folders)
	sort.Slice(out.Objects, func(i, j int) bool { return out.Objects[i].Key < out.Objects[j].Key })
	return out, nil
}

func (f *fakeStore) Put(_ context.Context, bucket, key string, r io.Reader, _ int64, _ string) error {
	content, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.objects[bucket]; !ok {
		return objectstore.ErrBucketNotFound
	}
	f.objects[bucket][key] = content
	return nil
}

func (f *fakeStore) Get(_ context.Context, bucket, key string) (io.ReadCloser, objectstore.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	content, ok := f.objects[bucket][key]
	if !ok {
		return nil, objectstore.Object{}, objectstore.ErrObjectNotFound
	}
	return io.NopCloser(strings.NewReader(string(content))),
		objectstore.Object{Key: key, Size: int64(len(content))}, nil
}

func (f *fakeStore) Remove(_ context.Context, bucket, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects[bucket], key)
	return nil
}

func (f *fakeStore) RemoveAll(_ context.Context, bucket, prefix string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	removed := 0
	for key := range f.objects[bucket] {
		if strings.HasPrefix(key, prefix) {
			delete(f.objects[bucket], key)
			removed++
		}
	}
	return removed, nil
}

// link adds an external store with keys typed in place of a stored
// account, which is the path somebody takes on a fresh instance.
func link(t *testing.T, f *servertest.Fixture, name string, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{
		"kind": "external", "name": name, "provider": "generic",
		"endpoint":       "https://s3.wasabisys.com",
		"new_access_key": "AKIAEXAMPLE", "new_secret_key": "s3cr3t-example",
	}
	for k, v := range extra {
		body[k] = v
	}
	rec := f.Do(t, http.MethodPost, "/objectstores", body, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("link %q: %d %s", name, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode store: %v", err)
	}
	return out
}

// linkScoped links a store pinned to one bucket.
//
// It is a Space rather than the generic endpoint every other test uses,
// which is the shape somebody actually arrives in: a Space's access
// keys are routinely issued for one bucket, so it is where the field is
// the expected answer. Any provider takes it — see
// TestABucketMayBeNamedOnAnyProvider.
func linkScoped(t *testing.T, f *servertest.Fixture, name, bucket string) map[string]any {
	t.Helper()
	return link(t, f, name, map[string]any{
		"provider": "digitalocean", "region": "nyc3", "endpoint": "", "bucket": bucket,
	})
}

func withFake(t *testing.T, buckets ...string) (*servertest.Fixture, *fakeStore) {
	t.Helper()
	f := servertest.New(t)
	fake := newFakeStore(buckets...)
	f.Server.ObjectStores.SetConnector(fake)
	return f, fake
}

// A member may see what storage the instance is wired to and may not
// see inside it. That split is the module's one authorization decision:
// Cubeship lets a member read a great deal about configuration and
// never lets one read data, and a bucket is data.
func TestAMemberSeesTheStoresAndNotWhatIsInThem(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	if rec := f.Do(t, http.MethodGet, "/objectstores", nil, memberKey); rec.Code != http.StatusOK {
		t.Fatalf("a member could not list the stores: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.Do(t, http.MethodGet, "/objectstores/offsite", nil, memberKey); rec.Code != http.StatusOK {
		t.Fatalf("a member could not read one store: %d %s", rec.Code, rec.Body.String())
	}

	closed := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/objectstores/offsite/buckets", nil},
		{http.MethodGet, "/objectstores/offsite/buckets/backups/objects", nil},
		{http.MethodGet, "/objectstores/offsite/buckets/backups/download?key=a.txt", nil},
		{http.MethodPut, "/objectstores/offsite/buckets/backups/objects?filename=a.txt", []byte("x")},
		{http.MethodDelete, "/objectstores/offsite/buckets/backups/objects?key=a.txt", nil},
		{http.MethodPost, "/objectstores/offsite/buckets", map[string]any{"name": "new"}},
		{http.MethodGet, "/objectstores/offsite/credentials", nil},
	}
	for _, c := range closed {
		rec := f.Do(t, c.method, c.path, c.body, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

// Upload, list, download, delete — the whole of what the dashboard
// does, through the real router.
func TestAFileGoesInAndComesBackOut(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)

	rec := f.Do(t, http.MethodPut,
		"/objectstores/offsite/buckets/backups/objects?prefix=2026/&filename=notes.txt",
		[]byte("hello"), f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	// The root of the bucket shows a folder, not the file: the file is
	// one level down, and a listing is one level.
	var listing objectstore.ListingResponse
	get(t, f, "/objectstores/offsite/buckets/backups/objects", &listing)
	if len(listing.Folders) != 1 || listing.Folders[0].Name != "2026" {
		t.Fatalf("root listing = %+v, want one folder named 2026", listing)
	}
	if len(listing.Objects) != 0 {
		t.Errorf("root listing showed %d objects, want none", len(listing.Objects))
	}

	get(t, f, "/objectstores/offsite/buckets/backups/objects?prefix=2026/", &listing)
	if len(listing.Objects) != 1 || listing.Objects[0].Name != "notes.txt" {
		t.Fatalf("folder listing = %+v, want notes.txt", listing)
	}
	if want := "2026/notes.txt"; listing.Objects[0].Key != want {
		t.Errorf("key = %q, want %q — the key is what every other call takes", listing.Objects[0].Key, want)
	}

	rec = f.Do(t, http.MethodGet,
		"/objectstores/offsite/buckets/backups/download?key=2026/notes.txt", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello" {
		t.Errorf("downloaded %q, want %q", rec.Body.String(), "hello")
	}
	// Never inline. A bucket holds whatever the apps on this instance
	// put there, and an uploaded HTML file rendered here would run on
	// this daemon's own origin, beside the session cookie.
	if disposition := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment;") {
		t.Errorf("Content-Disposition = %q, want an attachment", disposition)
	}

	rec = f.Do(t, http.MethodDelete,
		"/objectstores/offsite/buckets/backups/objects?key=2026/notes.txt", nil, f.AdminKey)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	get(t, f, "/objectstores/offsite/buckets/backups/objects?prefix=2026/", &listing)
	if len(listing.Objects) != 0 {
		t.Errorf("the object survived its own delete: %+v", listing.Objects)
	}
}

// A folder is a common prefix, so deleting one means deleting
// everything under it — and an empty prefix would therefore mean the
// whole bucket. That is exactly what a query parameter going missing
// looks like, so it is refused rather than obeyed.
func TestDeletingAFolderTakesWhatIsUnderItAndNeverTheWholeBucket(t *testing.T) {
	f, fake := withFake(t, "backups")
	link(t, f, "offsite", nil)

	for _, key := range []string{"2026/a.txt", "2026/deep/b.txt", "2025/c.txt"} {
		if err := fake.Put(context.Background(), "backups", key, strings.NewReader("x"), 1, ""); err != nil {
			t.Fatal(err)
		}
	}

	rec := f.Do(t, http.MethodDelete,
		"/objectstores/offsite/buckets/backups/folders", nil, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("deleting with no prefix: %d %s, want 400", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodDelete,
		"/objectstores/offsite/buckets/backups/folders?prefix=2026/", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete folder: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Removed int `json:"removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Removed != 2 {
		t.Errorf("removed %d, want 2 — a folder is everything under it, at every depth", out.Removed)
	}
	if _, ok := fake.objects["backups"]["2025/c.txt"]; !ok {
		t.Error("the neighbouring folder went too")
	}
}

// An empty folder is a zero-byte object whose key ends in a slash,
// which is the convention every console uses. What matters is that the
// marker does not then appear as a nameless file inside the folder it
// creates.
func TestAnEmptyFolderIsAMarkerThatDoesNotShowUpAsAFile(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)

	rec := f.Do(t, http.MethodPost, "/objectstores/offsite/buckets/backups/folders",
		map[string]any{"name": "empty"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create folder: %d %s", rec.Code, rec.Body.String())
	}

	var listing objectstore.ListingResponse
	get(t, f, "/objectstores/offsite/buckets/backups/objects", &listing)
	if len(listing.Folders) != 1 || listing.Folders[0].Name != "empty" {
		t.Fatalf("root listing = %+v, want one folder named empty", listing)
	}
	get(t, f, "/objectstores/offsite/buckets/backups/objects?prefix=empty/", &listing)
	if len(listing.Objects) != 0 || len(listing.Folders) != 0 {
		t.Errorf("the folder contains its own marker: %+v", listing)
	}
}

// A store pinned to one bucket is pinned in both directions: it reports
// that bucket without asking the endpoint — its login may not list
// them — and it refuses to reach any other. Without the second half,
// typing another name into the URL would get an access-denied from the
// provider that nobody can act on.
func TestAStorePinnedToOneBucketReachesNoOther(t *testing.T) {
	f, fake := withFake(t, "backups", "somebody-elses")
	linkScoped(t, f, "scoped", "backups")

	var buckets []objectstore.BucketResponse
	get(t, f, "/objectstores/scoped/buckets", &buckets)
	if len(buckets) != 1 || buckets[0].Name != "backups" {
		t.Fatalf("buckets = %+v, want only backups", buckets)
	}
	// Answered from the row, so a login that cannot list buckets never
	// has to.
	if buckets[0].CreatedAt != nil {
		t.Error("a pinned bucket reported a creation time, so the endpoint was asked after all")
	}

	for _, path := range []string{
		"/objectstores/scoped/buckets/somebody-elses/objects",
		"/objectstores/scoped/buckets/somebody-elses/download?key=a.txt",
	} {
		if rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: %d, want 400", path, rec.Code)
		}
	}
	if len(fake.objects["somebody-elses"]) != 0 {
		t.Error("the other bucket was touched")
	}
}

// The store said no, not Cubeship. They are different sentences and
// they send somebody to different places: one is "ask an admin", the
// other is "the key we hold is wrong".
func TestARefusalFromTheStoreIsNotARefusalFromCubeship(t *testing.T) {
	f, fake := withFake(t, "backups")
	link(t, f, "offsite", nil)
	fake.denied = true

	rec := f.Do(t, http.MethodGet, "/objectstores/offsite/buckets", nil, f.AdminKey)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied by the store: %d %s, want 403", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "refused this login") {
		t.Errorf("the refusal reads as Cubeship's rather than the store's: %q", body)
	}
}

// A credential is a convenience, not a prerequisite: keys typed while
// linking a store become an account, in the same transaction, and turn
// up under Credentials ready to be picked for the next one.
func TestKeysTypedWhileLinkingBecomeAnAccountNothingCanThenStrand(t *testing.T) {
	f, _ := withFake(t, "backups")
	store := link(t, f, "offsite", map[string]any{"label": "Wasabi"})

	credentialID, ok := store["credential_id"].(float64)
	if !ok || credentialID == 0 {
		t.Fatalf("the linked store carries no credential: %+v", store)
	}

	var creds []map[string]any
	get(t, f, "/credentials", &creds)
	var found map[string]any
	for _, c := range creds {
		if c["label"] == "Wasabi" {
			found = c
		}
	}
	if found == nil {
		t.Fatalf("the typed keys did not turn up under credentials: %+v", creds)
	}
	if found["password"] != nil {
		t.Error("the secret came back out of the credentials listing")
	}

	// And the store standing on it is what stops the account being
	// deleted out from under it.
	rec := f.Do(t, http.MethodDelete, "/credentials/"+itoa(found["id"]), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("deleting the account under a store: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "object store offsite") {
		t.Errorf("the refusal does not name what stands on it: %q", body)
	}

	// Once the store goes, the account is ordinary again.
	if rec := f.Do(t, http.MethodDelete, "/objectstores/offsite", nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("delete store: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.Do(t, http.MethodDelete, "/credentials/"+itoa(found["id"]), nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("delete the freed account: %d %s", rec.Code, rec.Body.String())
	}
}
