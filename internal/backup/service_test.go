package backup_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/backup"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
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

// With nothing linked, a backup still happens — on this machine's own
// disk, which is not a backup and says so. That is deliberate: an
// instance that could take none at all until somebody had linked a
// bucket is worse.
func TestWithNothingLinkedABackupStillHappensAndSaysWhereItIs(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	row := f.take(t, "pg")
	if row.Status != "succeeded" {
		t.Fatalf("the backup came back %q: %s", row.Status, row.Error)
	}
	if row.OffMachine || row.Store != "" {
		t.Errorf("a dump on this machine's own disk reports off_machine=%v store=%q",
			row.OffMachine, row.Store)
	}

	rec := f.Do(t, http.MethodGet, "/backups/"+itoa(row.ID)+"/download", nil, f.AdminKey)
	if rec.Code != http.StatusOK || rec.Body.String() != "-- a dump\n" {
		t.Fatalf("download from the local disk: %d %q", rec.Code, rec.Body.String())
	}
}

// A dump the engine refused is a failed backup, not a failed request —
// and the row is what says so. It is kept, because a backup that did
// not happen is the thing somebody most needs to find out about, and
// what the engine said is carried into it: "exit status 1" is not a
// reason anybody can act on.
//
// Nothing is left in the bucket. A half-uploaded dump left behind is
// one somebody restores from.
func TestADumpThatFailsKeepsTheRowAndLeavesNothingBehind(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "keep": 7, "store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("set a schedule: %d", code)
	}
	f.docker.set("", 1, "pg_dump: error: connection to server failed")

	row := f.take(t, "pg")
	if row.Status != "failed" {
		t.Fatalf("a dump the engine refused came back %q", row.Status)
	}
	if !strings.Contains(row.Error, "connection to server failed") {
		t.Errorf("the row says %q, and what the engine said is the only reason anybody can act on", row.Error)
	}
	if keys := f.store.keys("dumps"); len(keys) != 0 {
		t.Errorf("the bucket holds %v after a failed dump", keys)
	}

	// Nothing complete is at the other end, so there is nothing to load
	// back — said as a refusal rather than as a half-replaced database.
	if rec := f.Do(t, http.MethodPost, "/backups/"+itoa(row.ID)+"/restore", nil, f.AdminKey); rec.Code != http.StatusConflict {
		t.Errorf("restoring from a failed backup answered %d, want 409", rec.Code)
	}
}

// An empty dump is never right — every engine writes a header even for
// an empty database — and it is the shape a silent failure takes. A
// backup that says it failed beats one that restores to nothing.
func TestAnEmptyDumpIsAFailureRatherThanAnEmptyBackup(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.docker.set("", 0, "")

	row := f.take(t, "pg")
	if row.Status != "failed" {
		t.Fatalf("a dump of nothing at all came back %q, size %d", row.Status, row.Size)
	}
}

// Deleting a database is exactly the moment its backups matter, so they
// outlive it — which only works because the name, the engine and the
// version are written down rather than joined.
//
// What cannot be done is restoring one: where it should go is a choice
// this release does not offer, and picking one would be the wrong
// database quietly replaced.
func TestABackupOutlivesTheDatabaseItCameFrom(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	row := f.take(t, "pg")

	if rec := f.Do(t, http.MethodDelete, "/datastores/pg", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Fatalf("delete the database: %d %s", rec.Code, rec.Body.String())
	}

	rows := f.backups(t, "")
	if len(rows) != 1 {
		t.Fatalf("the instance holds %d backups after its database went, want 1", len(rows))
	}
	orphan := rows[0]
	if orphan.ID != row.ID {
		t.Fatalf("the backup that survived is #%d, not the one taken", orphan.ID)
	}
	if orphan.Exists {
		t.Error("the row still claims its database is here")
	}
	if orphan.Database != "pg" || orphan.Engine != "postgres" || orphan.Version == "" {
		t.Errorf("the orphan cannot say what produced it: %+v", orphan)
	}

	if rec := f.Do(t, http.MethodPost, "/backups/"+itoa(orphan.ID)+"/restore", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("restoring an orphan answered %d, want 404", rec.Code)
	}
	// It can still be fetched, which is the point of keeping it.
	if rec := f.Do(t, http.MethodGet, "/backups/"+itoa(orphan.ID)+"/download", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Errorf("downloading an orphan answered %d", rec.Code)
	}
}

// The rule retention exists to keep: **a run of failures must not push
// the last good dump out of the window.** Counted naively — newest
// three rows, whatever they are — two failed nights are enough to make
// the only restorable backup on the instance expire, at exactly the
// moment somebody is going to need it.
func TestARunOfFailuresNeverPushesOutTheLastGoodDump(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "keep": 2, "store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("set a schedule: %d", code)
	}

	good := f.take(t, "pg")
	f.docker.set("", 1, "pg_dump: error: could not connect")
	f.take(t, "pg")
	f.take(t, "pg")

	rows := f.backups(t, "pg")
	if len(rows) != 3 {
		t.Fatalf("after one good night and two bad ones there are %d rows, want 3", len(rows))
	}
	if !held(rows, good.ID) {
		t.Fatal("two failures expired the only backup on this instance that can be restored")
	}

	// Two good nights later there is something to replace it with, and
	// only then does it go — object and row together.
	f.docker.set("-- a dump\n", 0, "")
	f.take(t, "pg")
	f.take(t, "pg")

	rows = f.backups(t, "pg")
	if held(rows, good.ID) {
		t.Error("keep=2 with three successes still holds the oldest")
	}
	if failures := countStatus(rows, "failed"); failures != 2 {
		t.Errorf("%d failed rows survived, want 2 — a failure is the evidence a schedule is not working", failures)
	}
	for _, key := range f.store.keys("dumps") {
		if key == good.Key {
			t.Error("the row was pruned and the object was left in the bucket, paid for and pointed at by nothing")
		}
	}
}

// A dump in flight is the one state a backup cannot be restored from or
// deleted in: something is still writing to it, and to the object it
// names.
//
// The row is written by hand because the only way to see this through
// the API is to catch a real dump mid-flight, which is a race a test
// should not be built on.
func TestADumpInFlightCannotBeRestoredOrDeleted(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	row, err := f.Server.Backups.Repo().Start(context.Background(), &backup.Backup{
		DatastoreID: f.datastoreID(t, "pg"), DatastoreName: "pg",
		Engine: "postgres", Version: "18",
		Key: backup.KeyFor("pg", time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if row.Status != "taking" {
		t.Fatalf("a row written before the work starts says %q", row.Status)
	}

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/backups/" + itoa(row.ID) + "/restore"},
		{http.MethodDelete, "/backups/" + itoa(row.ID)},
	} {
		if rec := f.Do(t, c.method, c.path, nil, f.AdminKey); rec.Code != http.StatusConflict {
			t.Errorf("%s %s while the dump is in flight answered %d, want 409", c.method, c.path, rec.Code)
		}
	}
}

// A dump is not loaded into an engine that did not produce it.
// `pg_dump` output is not MySQL's, and a major version apart is a
// restore that fails partway with the database already half replaced.
func TestADumpIsNotLoadedIntoAnEngineThatDidNotProduceIt(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")

	// Written by hand: an engine is fixed for the life of a datastore,
	// so the only rows that can disagree are ones that outlived a
	// change — which is exactly what this refusal is for.
	row, err := f.Server.Backups.Repo().Start(context.Background(), &backup.Backup{
		DatastoreID: f.datastoreID(t, "pg"), DatastoreName: "pg",
		Engine: "mysql", Version: "8.4",
		Key: backup.KeyFor("pg", time.Now().UTC()),
	})
	if err != nil {
		t.Fatalf("write the row: %v", err)
	}
	if err := f.Server.Backups.Repo().Finish(context.Background(), row.ID, 10, ""); err != nil {
		t.Fatalf("finish the row: %v", err)
	}

	rec := f.Do(t, http.MethodPost, "/backups/"+itoa(row.ID)+"/restore", nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("restoring a MySQL dump into Postgres answered %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mysql") {
		t.Errorf("the refusal does not say what the two are: %q", rec.Body.String())
	}
	if loaded := f.docker.loaded(); len(loaded) != 0 {
		t.Error("the refusal came after the engine had already been fed")
	}
}

// Redis is not backed up, and that is a decision rather than a gap. It
// is refused where somebody can read the reason, rather than offered a
// button that does nothing.
func TestAnEngineWithNoDumpIsRefusedWithTheReason(t *testing.T) {
	f := newFixture(t)
	f.database(t, "cache", "redis")

	rec := f.Do(t, http.MethodPost, "/datastores/cache/backups", nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("backing up Redis answered %d, want 409", rec.Code)
	}
	if code := f.schedule(t, "cache", map[string]any{"at": "03:00", "keep": 7}); code != http.StatusConflict {
		t.Errorf("scheduling Redis answered %d, want 409", code)
	}
}

// Every refusal a schedule can carry happens while the person who typed
// it is still watching, rather than at three in the morning in a log
// nobody reads.
func TestASchedduleIsRefusedBeforeTheTimerCanFailOnIt(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	f.linkStore(t, "offsite")

	refused := map[string]map[string]any{
		"a time that is not one":         {"at": "tonight"},
		"a time out of range":            {"at": "25:00"},
		"a zone this machine lacks":      {"at": "03:00", "timezone": "Mars/Olympus"},
		"a negative count":               {"at": "03:00", "keep": -1},
		"a count nobody meant":           {"at": "03:00", "keep": 100000},
		"a store with no bucket named":   {"at": "03:00", "store": "offsite"},
		"a store this instance does not": {"at": "03:00", "store": "nowhere", "bucket": "dumps"},
	}
	for name, body := range refused {
		code := f.schedule(t, "pg", body)
		if code == http.StatusOK {
			t.Errorf("%s was accepted", name)
		}
	}

	// A good one round-trips, and every surface names the store the way
	// the rest of this product does: by its name, never by its id.
	if code := f.schedule(t, "pg", map[string]any{
		"at": "03:00", "timezone": "America/Sao_Paulo", "keep": 7,
		"store": "offsite", "bucket": "dumps",
	}); code != http.StatusOK {
		t.Fatalf("a good schedule answered %d", code)
	}

	var got struct {
		At       string `json:"at"`
		Timezone string `json:"timezone"`
		Keep     int    `json:"keep"`
		Store    string `json:"store"`
		Bucket   string `json:"bucket"`
	}
	rec := f.Do(t, http.MethodGet, "/datastores/pg/backups/schedule", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read the schedule: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode the schedule: %v", err)
	}
	if got.At != "03:00" || got.Timezone != "America/Sao_Paulo" || got.Keep != 7 {
		t.Errorf("the schedule came back %+v", got)
	}
	if got.Store != "offsite" || got.Bucket != "dumps" {
		t.Errorf("the schedule names the store as %q/%q", got.Store, got.Bucket)
	}

	// Off is the row going, because the row existing is what scheduled
	// means. There is no flag that could say off with a time beside it.
	if rec := f.Do(t, http.MethodDelete, "/datastores/pg/backups/schedule", nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("unset the schedule: %d", rec.Code)
	}
	if rec := f.Do(t, http.MethodGet, "/datastores/pg/backups/schedule", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("a database with no schedule answered %d, want 404", rec.Code)
	}
}

// All of it is an admin's, reads included. A dump is every row in the
// database, so somebody who may take one may read everything it holds,
// and a restore replaces all of it — the line `objectstore` already
// draws around a bucket's contents.
func TestOnlyAnAdminMayReachABackup(t *testing.T) {
	f := newFixture(t)
	f.database(t, "pg", "postgres")
	row := f.take(t, "pg")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	id := itoa(row.ID)
	closed := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/backups", nil},
		{http.MethodGet, "/datastores/pg/backups", nil},
		{http.MethodPost, "/datastores/pg/backups", nil},
		{http.MethodGet, "/datastores/pg/backups/schedule", nil},
		{http.MethodPut, "/datastores/pg/backups/schedule", map[string]any{"at": "03:00"}},
		{http.MethodDelete, "/datastores/pg/backups/schedule", nil},
		{http.MethodPost, "/backups/" + id + "/restore", nil},
		{http.MethodGet, "/backups/" + id + "/download", nil},
		{http.MethodDelete, "/backups/" + id, nil},
	}
	for _, c := range closed {
		rec := f.Do(t, c.method, c.path, c.body, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

// --- small helpers ---

func held(rows []backupRow, id int64) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func countStatus(rows []backupRow, status string) int {
	n := 0
	for _, row := range rows {
		if row.Status == status {
			n++
		}
	}
	return n
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
