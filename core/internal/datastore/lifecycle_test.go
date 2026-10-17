package datastore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/datastore"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// A version this release does not offer is refused at creation, not
// discovered when the pull fails minutes later with nobody watching.
func TestAnUnknownEngineOrVersionIsRefused(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	for _, body := range []map[string]any{
		{"name": "a", "engine": "cockroach"},
		{"name": "b", "engine": "postgres", "version": "99"},
		{"name": "c", "engine": "mysql", "username": "root"},
	} {
		rec := f.Do(t, http.MethodPost, "/datastores", body, f.AdminKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %v: %d %s, want 400", body, rec.Code, rec.Body.String())
		}
	}
}

// Exposing is what puts a database on the internet, so the port it
// lands on has to be somebody's decision and no two may share one.
func TestExposingTakesAPortAndOnlyOneDatastoreGetsIt(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "my", "engine": "mysql"})

	rec := f.Do(t, http.MethodPost, "/datastores/pg/expose", map[string]any{}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("expose: %d %s", rec.Code, rec.Body.String())
	}
	var exposed struct {
		Port         int    `json:"exposed_port"`
		ExternalHost string `json:"external_host"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &exposed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exposed.Port < datastore.PortRangeStart || exposed.Port > datastore.PortRangeEnd {
		t.Errorf("automatic port %d is outside the range", exposed.Port)
	}
	if exposed.ExternalHost != servertest.Domain {
		t.Errorf("external host is %q, want the instance's domain", exposed.ExternalHost)
	}

	rec = f.Do(t, http.MethodPost, "/datastores/my/expose",
		map[string]any{"port": exposed.Port}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a second datastore took the same port: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodPost, "/datastores/my/expose",
		map[string]any{"port": 80}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("port 80 was accepted: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodDelete, "/datastores/pg/expose", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpose: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "exposed_port") {
		t.Errorf("still reports a port after being unexposed: %s", rec.Body.String())
	}
}

// One app, two databases, and the prefix is what keeps them from being
// one variable with two values.
func TestASecondDatabaseOnOneAppNeedsAPrefix(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "main", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "analytics", "engine": "postgres"})

	if rec := attach(t, f, "main", "web/production/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach the first: %d %s", rec.Code, rec.Body.String())
	}
	if rec := attach(t, f, "analytics", "web/production/api", ""); rec.Code != http.StatusConflict {
		t.Errorf("a second database landed on the same variables: %d %s", rec.Code, rec.Body.String())
	}
	if rec := attach(t, f, "analytics", "web/production/api", "ANALYTICS_"); rec.Code != http.StatusCreated {
		t.Fatalf("attach with a prefix: %d %s", rec.Code, rec.Body.String())
	}

	env := appEnv(t, f, "web/production/api")
	for _, key := range []string{"DATABASE_URL", "ANALYTICS_DATABASE_URL"} {
		if _, ok := env[key]; !ok {
			t.Errorf("the app did not inherit %s", key)
		}
	}
	if env["DATABASE_URL"].Value == env["ANALYTICS_DATABASE_URL"].Value {
		t.Error("both databases resolved to the same connection string")
	}
}

// A cache and a database on one app are not a collision.
//
// The prefix was doing the whole of that judgement, so attaching a
// Redis to an app that already had a Postgres was refused for a
// conflict that does not exist: one writes REDIS_URL and the other
// DATABASE_URL, and neither can overwrite the other. What has to be
// unique is the variable name, and the prefix is only half of it.
func TestOnlyDatabasesThatNameTheSameVariablesCollide(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createApp(t, f, "web", "production", "api")
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	createDatastore(t, f, map[string]any{"name": "cache", "engine": "redis"})
	createDatastore(t, f, map[string]any{"name": "docs", "engine": "mongodb"})

	for _, name := range []string{"pg", "cache", "docs"} {
		if rec := attach(t, f, name, "web/production/api", ""); rec.Code != http.StatusCreated {
			t.Fatalf("attach %s at no prefix: %d %s", name, rec.Code, rec.Body.String())
		}
	}

	env := appEnv(t, f, "web/production/api")
	for _, key := range []string{"DATABASE_URL", "REDIS_URL", "MONGO_URL"} {
		if _, ok := env[key]; !ok {
			t.Errorf("the app did not inherit %s: %v", key, env)
		}
	}

	// And two of the same kind still are one, at the same prefix.
	createDatastore(t, f, map[string]any{"name": "second", "engine": "mysql"})
	rec := attach(t, f, "second", "web/production/api", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second SQL database landed on DATABASE_URL: %d %s", rec.Code, rec.Body.String())
	}
	// The refusal names what collides. "That prefix is taken" leaves
	// somebody to work out which of three attachments it meant.
	if !strings.Contains(rec.Body.String(), "DATABASE_URL") {
		t.Errorf("the refusal does not name the variables: %s", rec.Body.String())
	}
}

// fakeDocker is a Docker that agrees to everything, so a test can watch
// a datastore go up, off and back on. servertest's own stub refuses
// every call, which is right for the tests that must not deploy by
// accident and useless for these.
type fakeDocker struct {
	mu      sync.Mutex
	running map[string]bool
	stopped []string
	// created is every container it was asked to make, and capped is
	// every ceiling it was asked to apply afterwards.
	created []dockerx.ContainerOpts
	capped  []dockerx.Resources

	// execOutput is what a dump "produces", execCode its exit status,
	// and restored collects what was fed back in.
	execOutput string
	execCode   int
	restored   []string
}

func newFakeDocker() *fakeDocker { return &fakeDocker{running: map[string]bool{}} }

func (f *fakeDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }

func (f *fakeDocker) SetResources(_ context.Context, _ string, r dockerx.Resources) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.capped = append(f.capped, r)
	return nil
}

// ceilings is every SetResources call, oldest first.
func (f *fakeDocker) ceilings() []dockerx.Resources {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dockerx.Resources(nil), f.capped...)
}

// createdWith is every container it was asked to make, oldest first.
func (f *fakeDocker) createdWith() []dockerx.ContainerOpts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dockerx.ContainerOpts(nil), f.created...)
}

func (f *fakeDocker) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, opts)
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
	f.stopped = append(f.stopped, name)
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

// ExecStream stands in for a dump: it writes whatever the test put in
// `execOutput` to stdout and reports success, so a backup can be taken
// end to end without a database anywhere.
func (f *fakeDocker) ExecStream(_ context.Context, _ string, _ []string, in io.Reader, out io.Writer) (string, int, error) {
	f.mu.Lock()
	output, code, restored := f.execOutput, f.execCode, f.restored != nil
	f.mu.Unlock()

	if in != nil {
		// A restore feeds stdin. Reading it to EOF is what the real
		// command does, and not doing it would leave the writer on the
		// other side blocked forever.
		read, _ := io.ReadAll(in)
		if restored {
			f.mu.Lock()
			f.restored = append(f.restored, string(read))
			f.mu.Unlock()
		}
	}
	if out != nil {
		if _, err := io.WriteString(out, output); err != nil {
			return "", 0, err
		}
	}
	return "", code, nil
}

func (f *fakeDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	// Docker frames stdout and stderr behind an 8-byte header; the
	// handler demultiplexes it, so a fake that returns raw bytes would
	// be testing the wrong thing.
	var framed bytes.Buffer
	line := []byte("database system is ready to accept connections\n")
	header := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	header[4] = byte(len(line) >> 24)
	header[5] = byte(len(line) >> 16)
	header[6] = byte(len(line) >> 8)
	header[7] = byte(len(line))
	framed.Write(header)
	framed.Write(line)
	return io.NopCloser(&framed), nil
}

// provisioned creates a datastore against a Docker that works and waits
// for it to come up.
func provisioned(t *testing.T, name string) (*servertest.Fixture, *fakeDocker) {
	t.Helper()
	docker := newFakeDocker()
	f := servertest.NewWithDocker(t, docker)
	// The provisioner watches a started container for a while before
	// calling it up, which is right on a real box and nine seconds of
	// nothing here.
	f.Server.Datastores.Provisioner().ReadyInterval = time.Millisecond

	createDatastore(t, f, map[string]any{"name": name, "engine": "postgres"})
	f.Server.Datastores.WaitForProvisioning()
	return f, docker
}

func statusOf(t *testing.T, f *servertest.Fixture, name string) string {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/datastores/"+name, nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("get datastore: %d %s", rec.Code, rec.Body.String())
	}
	var d struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return d.Status
}

// Turning a database off is a decision, and it has to keep looking like
// one. "down" is a container that stopped on its own; a screen that
// showed the same word for both would send somebody looking for a fault
// they created on purpose.
func TestStoppingADatabaseIsNotTheSameAsItFallingOver(t *testing.T) {
	dbtest.RequireDatabase(t)
	f, docker := provisioned(t, "pg")

	if got := statusOf(t, f, "pg"); got != "running" {
		t.Fatalf("a freshly provisioned datastore is %q", got)
	}

	rec := f.Do(t, http.MethodPost, "/datastores/pg/stop", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}
	if got := statusOf(t, f, "pg"); got != "stopped" {
		t.Errorf("after stopping, the datastore is %q, want stopped", got)
	}
	if len(docker.stopped) != 1 || docker.stopped[0] != "cubeship-db-pg" {
		t.Errorf("stopped %v, want the container by name", docker.stopped)
	}

	// The reconciler runs at every daemon start and sees a container
	// that is not running. Rewriting that to "down" would turn the
	// decision into what looks like a fault, on every restart.
	if err := datastore.Reconcile(t.Context(), f.Server.Datastores.Repo(), docker); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := statusOf(t, f, "pg"); got != "stopped" {
		t.Errorf("the reconciler rewrote a deliberate stop to %q", got)
	}

	// And back on. It is provisioned again rather than restarted, which
	// is one path instead of two.
	rec = f.Do(t, http.MethodPost, "/datastores/pg/start", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
	if got := statusOf(t, f, "pg"); got != "running" {
		t.Errorf("after starting, the datastore is %q, want running", got)
	}
}

// Turning a database off takes it away from every app attached to it,
// so it is an admin's. Reading its log is not: the reason it is
// refusing connections is in there, and finding that out is not a
// privilege.
func TestLogsAreAMembersAndStoppingIsAnAdmins(t *testing.T) {
	dbtest.RequireDatabase(t)
	f, _ := provisioned(t, "pg")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodGet, "/datastores/pg/logs", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read logs as a member: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ready to accept connections") {
		t.Errorf("logs came back as %q — the frame header was not demultiplexed", rec.Body.String())
	}

	for _, path := range []string{"/datastores/pg/stop", "/datastores/pg/start"} {
		if rec := f.Do(t, http.MethodPost, path, nil, memberKey); rec.Code != http.StatusForbidden {
			t.Errorf("POST %s as a member: %d, want 403", path, rec.Code)
		}
	}
}

// There is nothing to stop or to read a log from before a container
// exists, and saying so beats a 500 out of the Engine.
func TestStoppingAndLoggingNeedAContainer(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	createDatastore(t, f, map[string]any{"name": "pg", "engine": "postgres"})
	f.Server.Datastores.WaitForProvisioning()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/datastores/pg/stop"},
		{http.MethodGet, "/datastores/pg/logs"},
	} {
		rec := f.Do(t, c.method, c.path, nil, f.AdminKey)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s %s: %d %s, want 409", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	// Starting is how a failed provision is retried, so it is allowed.
	if rec := f.Do(t, http.MethodPost, "/datastores/pg/start", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Errorf("start after a failed provision: %d %s", rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
}

// **A database is the container on a box this size most worth capping**,
// and raising its ceiling must not be the same act as publishing a port:
// that one replaces the container, which is a database going away for a
// few seconds. This one does not.
func TestANewCeilingReachesTheDatabaseWithoutReplacingIt(t *testing.T) {
	f, docker := provisioned(t, "pg")
	before := len(docker.createdWith())

	var out struct {
		Limits struct {
			CPU    float64 `json:"cpu"`
			Memory int64   `json:"memory_bytes"`
		} `json:"limits"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/datastores/pg",
		map[string]any{"limits": map[string]any{"cpu": 2, "memory_bytes": 1 << 30}},
		f.AdminKey, &out), http.StatusOK)

	if out.Limits.CPU != 2 || out.Limits.Memory != 1<<30 {
		t.Errorf("it reports %+v", out.Limits)
	}
	ceilings := docker.ceilings()
	if len(ceilings) != 1 {
		t.Fatalf("the Engine was asked to set %d ceilings", len(ceilings))
	}
	want := dockerx.Resources{NanoCPUs: 2_000_000_000, MemoryBytes: 1 << 30}
	if ceilings[0] != want {
		t.Errorf("it was asked for %+v", ceilings[0])
	}
	if now := len(docker.createdWith()); now != before {
		t.Errorf("%d containers were created for a limit change", now-before)
	}
}

// The stored ceiling is what the next container is created under, so a
// database that is stopped and started does not come back uncapped.
func TestTheDatabaseComesBackUnderItsCeiling(t *testing.T) {
	f, docker := provisioned(t, "pg")
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/datastores/pg",
		map[string]any{"limits": map[string]any{"cpu": 1, "memory_bytes": 512 << 20}},
		f.AdminKey), http.StatusOK)

	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/datastores/pg/stop", nil, f.AdminKey), http.StatusOK)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/datastores/pg/start", nil, f.AdminKey), http.StatusOK)
	f.Server.Datastores.WaitForProvisioning()

	opts := docker.createdWith()
	if len(opts) == 0 {
		t.Fatal("nothing was created")
	}
	want := dockerx.Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 512 << 20}
	if got := opts[len(opts)-1].Resources; got != want {
		t.Errorf("it came back under %+v", got)
	}
}

// A number the Engine would refuse is refused here, where the person who
// typed it is still watching.
func TestADatabaseCeilingTooSmallToMeanAnythingIsRefused(t *testing.T) {
	f, _ := provisioned(t, "pg")
	for _, body := range []map[string]any{
		{"cpu": 0.001},
		{"memory_bytes": 1 << 20},
		{"cpu": -1},
	} {
		rec := f.Do(t, http.MethodPatch, "/datastores/pg",
			map[string]any{"limits": body}, f.AdminKey)
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
	}
}

func (*fakeDocker) PathOwner(context.Context, string, string) (dockerx.Owner, error) {
	return dockerx.Owner{Mode: 0o755}, nil
}
