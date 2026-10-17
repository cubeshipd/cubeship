package backup_test

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
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/server/servertest"
)

// fakeDocker is a Docker that agrees to everything and stands in for
// the engine's own dump command: it writes whatever the test put in
// `output` to stdout, and collects whatever a restore feeds to stdin.
//
// servertest's own stub refuses every call, which is right for tests
// that must not deploy by accident and useless here — a database has to
// be up before anything can be dumped out of it.
type fakeDocker struct {
	mu      sync.Mutex
	running map[string]bool

	output string
	// code is the dump command's exit status. Non-zero is an engine
	// refusing, which is a failed backup rather than a failed request.
	code int
	// stderr is what the engine said, which is the only place it
	// explains itself.
	stderr string
	// restored is every stream fed back into a container.
	restored []string
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{running: map[string]bool{}, output: "-- a dump\n"}
}

func (f *fakeDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }

func (f *fakeDocker) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	return opts.Name + "-id", nil
}

func (f *fakeDocker) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[id] = true
	return nil
}

func (f *fakeDocker) StopContainer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[name+"-id"] = false
	return nil
}

func (f *fakeDocker) RemoveContainer(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.running, name+"-id")
	return nil
}

func (f *fakeDocker) IsRunning(_ context.Context, id string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id], nil
}

func (f *fakeDocker) SetResources(context.Context, string, dockerx.Resources) error { return nil }

func (f *fakeDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeDocker) ExecStream(_ context.Context, _ string, _ []string, in io.Reader, out io.Writer) (string, int, error) {
	f.mu.Lock()
	output, code, stderr := f.output, f.code, f.stderr
	f.mu.Unlock()

	if in != nil {
		// A restore feeds stdin. Reading it to EOF is what the real
		// command does, and not doing it leaves the writer on the other
		// side of the pipe blocked forever.
		read, _ := io.ReadAll(in)
		f.mu.Lock()
		f.restored = append(f.restored, string(read))
		f.mu.Unlock()
		return stderr, code, nil
	}
	if out != nil {
		if _, err := io.WriteString(out, output); err != nil {
			return "", 0, err
		}
	}
	return stderr, code, nil
}

func (f *fakeDocker) set(output string, code int, stderr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.output, f.code, f.stderr = output, code, stderr
}

func (f *fakeDocker) loaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.restored...)
}

// fakeStore is an S3 endpoint in a map — the same bargain
// objectstore's own tests make. What is worth testing about a backup
// landing in a bucket is that it arrives, that a failure leaves nothing
// behind, and that pruning takes the object as well as the row.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string]map[string][]byte
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
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []objectstore.Bucket
	for name := range f.objects {
		out = append(out, objectstore.Bucket{Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) MakeBucket(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[name] = map[string][]byte{}
	return nil
}

func (f *fakeStore) RemoveBucket(context.Context, string) error { return nil }

func (f *fakeStore) List(_ context.Context, bucket, prefix, _ string, _ int) (objectstore.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys, ok := f.objects[bucket]
	if !ok {
		return objectstore.Listing{}, objectstore.ErrBucketNotFound
	}
	out := objectstore.Listing{Prefix: prefix}
	for key, content := range keys {
		if strings.HasPrefix(key, prefix) {
			out.Objects = append(out.Objects, objectstore.Object{Key: key, Size: int64(len(content))})
		}
	}
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

func (f *fakeStore) RemoveAll(context.Context, string, string) (int, error) { return 0, nil }

func (f *fakeStore) keys(bucket string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for key := range f.objects[bucket] {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// --- the fixture ---

type fixture struct {
	*servertest.Fixture
	docker *fakeDocker
	store  *fakeStore
}

// newFixture is a wired instance with a Docker that answers and a
// bucket in a map. Nothing is created in it: each test says which
// databases and which store it wants.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dbtest.RequireDatabase(t)

	docker := newFakeDocker()
	store := newFakeStore("dumps")
	f := servertest.NewWithDocker(t, docker)
	f.Server.Datastores.Provisioner().ReadyInterval = time.Millisecond
	f.Server.ObjectStores.Provisioner().ReadyInterval = time.Millisecond
	f.Server.ObjectStores.SetConnector(store)

	return &fixture{Fixture: f, docker: docker, store: store}
}

func (f *fixture) database(t *testing.T, name, engine string) {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/datastores",
		map[string]any{"name": name, "engine": engine}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s: %d %s", name, rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
}

func (f *fixture) linkStore(t *testing.T, name string) {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "external", "name": name, "provider": "generic",
		"endpoint":       "https://s3.example.com",
		"new_access_key": "AKIAEXAMPLE", "new_secret_key": "s3cr3t",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("link %s: %d %s", name, rec.Code, rec.Body.String())
	}
}

// take asks for one and waits for the detached dump to finish, so a
// test asserts on an outcome rather than on a row that is still being
// written.
func (f *fixture) take(t *testing.T, database string) backupRow {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/datastores/"+database+"/backups", nil, f.AdminKey)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("take a backup: %d %s", rec.Code, rec.Body.String())
	}
	f.Server.Backups.Wait()

	var row backupRow
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatalf("decode the backup: %v", err)
	}
	for _, after := range f.backups(t, database) {
		if after.ID == row.ID {
			return after
		}
	}
	t.Fatalf("the backup this instance just took is not in its own listing")
	return row
}

// backupRow mirrors backup.Response, which is what every surface here
// speaks.
type backupRow struct {
	ID         int64  `json:"id"`
	Database   string `json:"database"`
	Exists     bool   `json:"database_exists"`
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	Store      string `json:"store"`
	Bucket     string `json:"bucket"`
	Key        string `json:"key"`
	OffMachine bool   `json:"off_machine"`
	Size       int64  `json:"size_bytes"`
	Status     string `json:"status"`
	Error      string `json:"error"`
	Scheduled  bool   `json:"scheduled"`
}

func (f *fixture) backups(t *testing.T, database string) []backupRow {
	t.Helper()
	path := "/backups"
	if database != "" {
		path = "/datastores/" + database + "/backups"
	}
	var rows []backupRow
	rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list backups: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode backups: %v", err)
	}
	return rows
}

func (f *fixture) schedule(t *testing.T, database string, body map[string]any) int {
	t.Helper()
	rec := f.Do(t, http.MethodPut, "/datastores/"+database+"/backups/schedule", body, f.AdminKey)
	return rec.Code
}

func (f *fixture) datastoreID(t *testing.T, name string) int64 {
	t.Helper()
	d, err := f.Server.Datastores.BySlug(context.Background(), name)
	if err != nil {
		t.Fatalf("look up %s: %v", name, err)
	}
	return d.ID
}

// --- the tests ---

// The whole feature in one pass: a database is dumped, the dump lands
// in a bucket rather than on this machine, it can be fetched back
// byte for byte, and it can be loaded into the database it came from.
func TestABackupGoesOutToABucketAndComesBackIn(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")
	f.docker.set("-- pg_dump output\nCREATE TABLE t;\n", 0, "")

	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "timezone": "UTC", "keep": 7, "store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("set a schedule: %d", code)
	}

	row := f.take(t, "pg")
	if row.Status != "succeeded" {
		t.Fatalf("the backup came back %q: %s", row.Status, row.Error)
	}
	if !row.OffMachine || row.Store != "offsite" || row.Bucket != "dumps" {
		t.Errorf("the backup says it is at %q/%q off_machine=%v; the schedule named a bucket",
			row.Store, row.Bucket, row.OffMachine)
	}
	if row.Size != int64(len("-- pg_dump output\nCREATE TABLE t;\n")) {
		t.Errorf("size is %d, and nothing counted the bytes that actually went past", row.Size)
	}
	if row.Engine != "postgres" || row.Version == "" {
		t.Errorf("the dump does not say what produced it: %q %q", row.Engine, row.Version)
	}
	if keys := f.store.keys("dumps"); len(keys) != 1 || keys[0] != row.Key {
		t.Errorf("the bucket holds %v, and the row names %q", keys, row.Key)
	}

	// Downloading is half of what makes a backup worth having: one
	// nobody can get at is only useful to this instance.
	rec := f.Do(t, http.MethodGet, "/backups/"+itoa(row.ID)+"/download", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "-- pg_dump output\nCREATE TABLE t;\n" {
		t.Errorf("what came back is %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("Content-Disposition is %q; a dump is somebody's data and is never rendered inline", got)
	}

	if rec := f.Do(t, http.MethodPost, "/backups/"+itoa(row.ID)+"/restore", nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	loaded := f.docker.loaded()
	if len(loaded) != 1 || loaded[0] != "-- pg_dump output\nCREATE TABLE t;\n" {
		t.Errorf("the restore fed the engine %q", loaded)
	}
}
