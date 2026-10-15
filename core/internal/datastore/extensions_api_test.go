package datastore_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"cubeship/internal/datastore"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// The API and the column, end to end. extension_test.go beside this one
// covers the rules; this covers the row, the routes and who may use them.

// quick is a fixture whose provisioner does not sleep. The intervals are
// right on a real box and are nine seconds of nothing here.
func quick(t *testing.T, docker *fakeDocker) *servertest.Fixture {
	t.Helper()
	f := servertest.NewWithDocker(t, docker)
	p := f.Server.Datastores.Provisioner()
	p.ReadyInterval = time.Millisecond
	p.ExtensionInterval = time.Millisecond
	p.ExtensionAttempts = 3
	return f
}

// extList reads the `extensions` field out of a decoded response.
func extList(t *testing.T, value any) []string {
	t.Helper()
	raw, ok := value.([]any)
	if !ok {
		t.Fatalf("extensions is %#v, want a list", value)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

// What is stored is the normalized list, not what was typed: sorted,
// deduplicated, and carrying what an extension requires.
func TestCreatingWithExtensionsStoresTheNormalizedList(t *testing.T) {
	docker := newFakeDocker()
	f := quick(t, docker)
	created := createDatastore(t, f, map[string]any{
		"name": "vec", "engine": "postgres", "version": "16",
		"extensions": []string{"vectorchord", "vectorchord"},
	})
	f.Server.Datastores.WaitForProvisioning()

	want := []string{"pgvector", "vectorchord"}
	if got := extList(t, created["extensions"]); !slices.Equal(got, want) {
		t.Fatalf("created with %v, want %v", got, want)
	}
	// And it survives the round trip through the column, which is the
	// half a create response cannot prove.
	var read map[string]any
	f.DoJSON(t, http.MethodGet, "/datastores/vec", nil, f.AdminKey, &read)
	if got := extList(t, read["extensions"]); !slices.Equal(got, want) {
		t.Fatalf("read back %v, want %v", got, want)
	}

	// The container runs the image the combination chose, pinned.
	created2 := docker.createdWith()
	last := created2[len(created2)-1]
	if !strings.Contains(last.Image, "vchord-postgres") || !strings.Contains(last.Image, "@sha256:") {
		t.Errorf("it runs %q", last.Image)
	}
	if !slices.Contains(last.Cmd, "shared_preload_libraries=vchord,vector") {
		t.Errorf("VectorChord's library is not preloaded: %v", last.Cmd)
	}
}

// Every datastore nobody asked any of, which is every database an
// upgrading instance already has, reads as `[]` and keeps its image.
func TestADatabaseWithoutExtensionsIsUnchanged(t *testing.T) {
	docker := newFakeDocker()
	f := quick(t, docker)
	created := createDatastore(t, f, map[string]any{
		"name": "plain", "engine": "postgres", "version": "16"})
	f.Server.Datastores.WaitForProvisioning()

	value, ok := created["extensions"]
	if !ok || value == nil {
		t.Fatalf("extensions is %#v, want an empty list rather than null", value)
	}
	if got := extList(t, value); len(got) != 0 {
		t.Fatalf("got %v", got)
	}

	var list []map[string]any
	f.DoJSON(t, http.MethodGet, "/datastores", nil, f.AdminKey, &list)
	if len(list) != 1 || list[0]["extensions"] == nil {
		t.Fatalf("a listing dropped the field: %v", list)
	}

	opts := docker.createdWith()
	if got := opts[len(opts)-1].Image; got != "postgres:16" {
		t.Errorf("it runs %q, want the plain image", got)
	}
	// And nothing was run inside it: the path costs a database with no
	// extensions not even a `docker exec`.
	if opts[len(opts)-1].Cmd != nil {
		t.Errorf("it took a command: %v", opts[len(opts)-1].Cmd)
	}
}

// The query that reads an app's environment joins two tables and has a
// Scan of its own. A column added to the list without being added there
// is how every read of an app's environment once became "expected 16
// destination arguments in Scan, not 14".
func TestTheJoinedReadCarriesTheExtensionColumn(t *testing.T) {
	f := quick(t, newFakeDocker())
	createDatastore(t, f, map[string]any{
		"name": "vec", "engine": "postgres", "version": "16",
		"extensions": []string{"pgvector"}})
	f.Server.Datastores.WaitForProvisioning()

	createApp(t, f, "web", "production", "api")
	if rec := attach(t, f, "vec", "web/production/api", ""); rec.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}
	if env := appEnv(t, f, "web/production/api"); env["DATABASE_URL"].Value == "" {
		t.Fatalf("the app has no connection string: %v", env)
	}
}

func TestCreatingRefusesWhatIsNotOffered(t *testing.T) {
	f := quick(t, newFakeDocker())
	for _, c := range []struct {
		name string
		body map[string]any
	}{
		{"a name nobody reviewed",
			map[string]any{"name": "a", "engine": "postgres", "extensions": []string{"postgis"}}},
		{"an image reference",
			map[string]any{"name": "b", "engine": "postgres", "extensions": []string{"ghcr.io/x/y:z"}}},
		{"a statement",
			map[string]any{"name": "c", "engine": "postgres", "extensions": []string{"vector; DROP TABLE users"}}},
		{"an engine with none",
			map[string]any{"name": "d", "engine": "redis", "extensions": []string{"pgvector"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := f.Do(t, http.MethodPost, "/datastores", c.body, f.AdminKey)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Installing on a database that already exists: the list grows, the
// container is replaced, and the row goes back to provisioning while it
// happens.
func TestInstallingOnAnExistingDatabase(t *testing.T) {
	docker := newFakeDocker()
	f := quick(t, docker)
	createDatastore(t, f, map[string]any{"name": "vec", "engine": "postgres", "version": "16"})
	f.Server.Datastores.WaitForProvisioning()
	before := len(docker.createdWith())

	var after map[string]any
	rec := f.DoJSON(t, http.MethodPost, "/datastores/vec/extensions",
		map[string]any{"extensions": []string{"vectorchord"}}, f.AdminKey, &after)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	// What it requires came with it.
	if got := extList(t, after["extensions"]); !slices.Equal(got, []string{"pgvector", "vectorchord"}) {
		t.Fatalf("now has %v", got)
	}
	if after["status"] != datastore.StatusProvisioning {
		t.Errorf("status is %v, want provisioning while the container is replaced", after["status"])
	}

	f.Server.Datastores.WaitForProvisioning()
	opts := docker.createdWith()
	if len(opts) <= before {
		t.Fatal("the container was not replaced")
	}
	last := opts[len(opts)-1]
	if last.Image == "postgres:16" {
		t.Errorf("the replacement runs the plain image: %s", last.Image)
	}
	if last.Labels["cubeship.extensions"] != "pgvector,vectorchord" {
		t.Errorf("the replacement is not labelled: %v", last.Labels)
	}
	if statusOf(t, f, "vec") != datastore.StatusRunning {
		t.Errorf("it did not come back up: %s", statusOf(t, f, "vec"))
	}
}

// Asking for what is already there is not a change, so nothing is
// replaced — which is what makes this safe to call from a script that
// does not track state.
func TestInstallingWhatIsAlreadyThereChangesNothing(t *testing.T) {
	docker := newFakeDocker()
	f := quick(t, docker)
	createDatastore(t, f, map[string]any{
		"name": "vec", "engine": "postgres", "version": "16",
		"extensions": []string{"pgvector"}})
	f.Server.Datastores.WaitForProvisioning()
	before := len(docker.createdWith())

	rec := f.Do(t, http.MethodPost, "/datastores/vec/extensions",
		map[string]any{"extensions": []string{"pgvector"}}, f.AdminKey)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	f.Server.Datastores.WaitForProvisioning()
	if got := len(docker.createdWith()); got != before {
		t.Errorf("it replaced the container anyway: %d then %d", before, got)
	}
}

// Removing is refused rather than ignored: the data may already depend
// on the extension, and the image that could tell you is the one being
// taken away.
func TestAnExtensionCannotBeRemoved(t *testing.T) {
	f := quick(t, newFakeDocker())
	createDatastore(t, f, map[string]any{
		"name": "vec", "engine": "postgres", "version": "16",
		"extensions": []string{"pgvector", "vectorchord"}})
	f.Server.Datastores.WaitForProvisioning()

	// Through the service, because the endpoint only ever adds what it
	// is given to what is there — this is the one way to ask.
	_, err := f.Server.Datastores.Update(context.Background(), f.Admin, "vec",
		nil, nil, &[]string{"pgvector"})
	if err == nil {
		t.Fatal("an extension was dropped")
	}
	if statusOf(t, f, "vec") != datastore.StatusRunning {
		t.Error("a refusal disturbed the database")
	}
}

// PATCH is for the description and the limits. Extensions have their own
// endpoint because adding one replaces the container, and a request that
// asked for one and got a 200 would be somebody believing their database
// has it.
func TestPatchWillNotChangeExtensions(t *testing.T) {
	f := quick(t, newFakeDocker())
	createDatastore(t, f, map[string]any{"name": "vec", "engine": "postgres", "version": "16"})
	f.Server.Datastores.WaitForProvisioning()

	rec := f.Do(t, http.MethodPatch, "/datastores/vec",
		map[string]any{"extensions": []string{"pgvector"}}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
	// Sending exactly what is there is not a change, so it is not an
	// error either: a client that always sends the whole object works.
	rec = f.Do(t, http.MethodPatch, "/datastores/vec",
		map[string]any{"extensions": []string{}, "description": "vectors"}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
}

// The same role as every other write here. A member deploys images
// somebody else published; replacing a running database is an admin's.
func TestInstallingNeedsTheAdminRole(t *testing.T) {
	f := quick(t, newFakeDocker())
	createDatastore(t, f, map[string]any{"name": "vec", "engine": "postgres", "version": "16"})
	f.Server.Datastores.WaitForProvisioning()

	_, memberKey := f.AddMember(t, "dev", user.RoleMember)
	rec := f.Do(t, http.MethodPost, "/datastores/vec/extensions",
		map[string]any{"extensions": []string{"pgvector"}}, memberKey)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a member installed an extension: %d %s", rec.Code, rec.Body.String())
	}
	// Reading is still a member's: what a database has is part of
	// knowing what you are deploying against.
	if rec := f.Do(t, http.MethodGet, "/datastores/vec", nil, memberKey); rec.Code != http.StatusOK {
		t.Fatalf("a member cannot read the database: %d", rec.Code)
	}
}

// The engines endpoint is where a client learns what it may ask for, and
// at which versions — so a form never offers a combination creation
// would refuse.
func TestTheEnginesEndpointNamesTheExtensions(t *testing.T) {
	f := quick(t, newFakeDocker())

	var engines []map[string]any
	f.DoJSON(t, http.MethodGet, "/datastores/engines", nil, f.AdminKey, &engines)
	if len(engines) == 0 {
		t.Fatal("no engines")
	}

	for _, e := range engines {
		list, _ := e["extensions"].([]any)
		if e["engine"] != "postgres" {
			if len(list) != 0 {
				t.Errorf("%v offers %v", e["engine"], list)
			}
			continue
		}
		if len(list) != len(datastore.AllExtensions()) {
			t.Fatalf("postgres offers %d extensions, the daemon knows %d",
				len(list), len(datastore.AllExtensions()))
		}
		first, _ := list[0].(map[string]any)
		if first["name"] != "pgvector" || first["sql_name"] != "vector" {
			t.Errorf("the public name and the SQL name are not both reported: %v", first)
		}
		if versions, _ := first["versions"].([]any); len(versions) == 0 {
			t.Error("an extension with no versions is one nothing can be created with")
		}
	}
}

// sqlFailingDocker is a container where the engine comes up and refuses
// the statement — the failure that matters, because the container is
// gone by the time anybody goes looking for its log.
type sqlFailingDocker struct{ *fakeDocker }

func (d *sqlFailingDocker) ExecStream(_ context.Context, _ string, cmd []string, _ io.Reader, out io.Writer) (string, int, error) {
	if cmd[0] == "pg_isready" {
		return "", 0, nil
	}
	if out != nil {
		_, _ = io.WriteString(out, `ERROR:  extension "vector" is not available`)
	}
	return "", 1, nil
}

// A failure creating an extension leaves the datastore failed, with the
// reason on its own row and no credential in it.
func TestAnExtensionThatWillNotInstallFailsTheDatabase(t *testing.T) {
	docker := &sqlFailingDocker{newFakeDocker()}
	f := servertest.NewWithDocker(t, docker)
	p := f.Server.Datastores.Provisioner()
	p.ReadyInterval = time.Millisecond
	p.ExtensionInterval = time.Millisecond

	created := createDatastore(t, f, map[string]any{
		"name": "vec", "engine": "postgres", "version": "16",
		"extensions": []string{"pgvector"}})
	f.Server.Datastores.WaitForProvisioning()

	var read map[string]any
	f.DoJSON(t, http.MethodGet, "/datastores/vec", nil, f.AdminKey, &read)
	if read["status"] != datastore.StatusFailed {
		t.Fatalf("status is %v, want failed", read["status"])
	}
	reason, _ := read["error"].(string)
	if reason == "" {
		t.Fatal("it failed without saying why")
	}
	if !strings.Contains(reason, "pgvector") {
		t.Errorf("the reason does not name the extension: %q", reason)
	}
	if !strings.Contains(reason, "not available") {
		t.Errorf("the reason does not carry what the engine said: %q", reason)
	}
	// The row is read by every surface, so a credential in it would be
	// a credential in a listing.
	password, _ := created["password"].(string)
	if password == "" {
		t.Fatal("the create response carried no password to check against")
	}
	if strings.Contains(reason, password) {
		t.Error("the failure carries the password")
	}
}
