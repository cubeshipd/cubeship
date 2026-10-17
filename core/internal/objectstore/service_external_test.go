package objectstore_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/envvar"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

// Both is not a request with an obvious reading, and neither is nothing.
// Guessing which was meant is how the wrong secret gets stored.
func TestLinkingTakesAStoredAccountOrTypedKeysAndNotBoth(t *testing.T) {
	f, _ := withFake(t)

	both := f.Do(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "external", "name": "two", "provider": "generic",
		"endpoint": "s3.example.com", "credential_id": 1,
		"new_access_key": "AKIA", "new_secret_key": "s3cr3t-example",
	}, f.AdminKey)
	if both.Code != http.StatusBadRequest {
		t.Errorf("a store linked with two logins: %d %s", both.Code, both.Body.String())
	}

	neither := f.Do(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "external", "name": "none", "provider": "generic",
		"endpoint": "s3.example.com",
	}, f.AdminKey)
	if neither.Code != http.StatusBadRequest {
		t.Errorf("a store linked with no login at all: %d %s", neither.Code, neither.Body.String())
	}
}

// `providers` is where this module's own API lists what it can link,
// and Go's mux prefers the literal — so a store called that would be a
// resource nothing could open. It is refused while the person who typed
// it is still there.
// Naming a bucket is accepted on every provider.
//
// It was refused everywhere but R2 and Spaces for a release, on the
// grounds that those are the ones whose logins are routinely issued per
// bucket — and that was a guess about somebody else's credential. An
// IAM policy can be narrowed to one and a compatible endpoint can be
// anything, so a login that reaches exactly one is a thing somebody can
// hold on any of them, and a store linked with one had nothing to show.
//
// What survives is `scopes_by_bucket`, which now decides what the form
// *says* rather than what the daemon takes. See the provider table in
// objectstore_test.go.
func TestABucketMayBeNamedOnAnyProvider(t *testing.T) {
	f := servertest.New(t)

	link := func(slug, provider string, extra map[string]any) int {
		body := map[string]any{
			"kind": "external", "name": slug, "provider": provider,
			"new_access_key": "AKIAEXAMPLE", "new_secret_key": "s3cr3t-example",
			"bucket": "one-bucket",
		}
		for k, v := range extra {
			body[k] = v
		}
		return f.Do(t, http.MethodPost, "/objectstores", body, f.AdminKey).Code
	}

	linked := []struct {
		slug, provider string
		extra          map[string]any
	}{
		// The case the field was built for: a token issued for one
		// bucket, which cannot list the others anyway.
		{"r2", "cloudflare", map[string]any{"account": "abc123"}},
		{"spaces", "digitalocean", map[string]any{"region": "nyc3"}},
		// And the two it used to refuse. An IAM policy narrowed to one
		// bucket is ordinary, and what a compatible endpoint's key
		// reaches is nobody here's guess to make.
		{"s3", "aws", map[string]any{"region": "eu-central-1"}},
		{"wasabi", "generic", map[string]any{"endpoint": "https://s3.wasabisys.com"}},
	}
	for _, c := range linked {
		if got := link(c.slug, c.provider, c.extra); got != http.StatusCreated {
			t.Errorf("linking %s with a bucket answered %d, want %d", c.provider, got, http.StatusCreated)
		}
	}

	// A name S3 would refuse is still refused, wherever it is typed.
	if got := link("bad", "generic", map[string]any{
		"endpoint": "https://s3.wasabisys.com", "bucket": "Not A Bucket",
	}); got != http.StatusBadRequest {
		t.Errorf("linking with an impossible bucket name answered %d, want 400", got)
	}
}

// Unpinning is the direction that can never be wrong, so it is accepted
// on any provider — including one whose logins are not issued per
// bucket, which is the only way out for a store pinned before that rule
// existed. Pinning keeps the rule linking keeps.
func TestAStoreCanBeUnpinnedWhateverItsProvider(t *testing.T) {
	f := servertest.New(t)

	var store map[string]any
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "external", "name": "r2", "provider": "cloudflare", "account": "abc123",
		"new_access_key": "AKIAEXAMPLE", "new_secret_key": "s3cr3t-example",
		"bucket": "one-bucket",
	}, f.AdminKey, &store), http.StatusCreated)
	if store["bucket"] != "one-bucket" {
		t.Fatalf("the store was not pinned: %+v", store)
	}

	var cleared map[string]any
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/objectstores/r2",
		map[string]any{"bucket": ""}, f.AdminKey, &cleared), http.StatusOK)
	if cleared["bucket"] != nil && cleared["bucket"] != "" {
		t.Errorf("the store is still pinned to %v", cleared["bucket"])
	}

	// And saving something else does not silently unpin it: the field
	// is a value rather than a gap, so leaving it out is the only way
	// of saying "as it is".
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/objectstores/r2",
		map[string]any{"bucket": "one-bucket"}, f.AdminKey), http.StatusOK)
	var described map[string]any
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/objectstores/r2",
		map[string]any{"description": "offsite"}, f.AdminKey, &described), http.StatusOK)
	if described["bucket"] != "one-bucket" {
		t.Errorf("saving a description unpinned the store: %+v", described["bucket"])
	}
}

func TestAStoreCannotBeCalledProviders(t *testing.T) {
	f, _ := withFake(t)
	rec := f.Do(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "managed", "name": "providers",
	}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a store called providers: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// A linked store is somebody else's, and forgetting it must not be
// confused with emptying it. Nothing in the bucket is touched.
func TestDeletingALinkedStoreLeavesTheBucketAlone(t *testing.T) {
	f, fake := withFake(t, "backups")
	link(t, f, "offsite", nil)
	if err := fake.Put(context.Background(), "backups", "keep.txt", strings.NewReader("x"), 1, ""); err != nil {
		t.Fatal(err)
	}

	if rec := f.Do(t, http.MethodDelete, "/objectstores/offsite", nil, f.AdminKey); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := fake.objects["backups"]["keep.txt"]; !ok {
		t.Error("forgetting a linked store emptied it")
	}
	if rec := f.Do(t, http.MethodGet, "/objectstores/offsite", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("the store survived its own delete: %d", rec.Code)
	}
}

// A managed store's series is served at the store's own address, like
// an app's and a database's — one module behind all three, and what
// differs is how the subject is resolved and who may ask.
//
// A linked store has no series at all and says so, rather than
// answering with an empty one: nothing here samples somebody else's
// server, and a chart of no samples reads as a store sitting idle.
func TestAManagedStoresMetricsAreAMembersAndALinkedOneHasNone(t *testing.T) {
	f, _ := withFake(t)
	rec := f.Do(t, http.MethodPost, "/objectstores", map[string]any{
		"kind": "managed", "name": "files",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create a managed store: %d %s", rec.Code, rec.Body.String())
	}
	link(t, f, "offsite", nil)

	_, memberKey := f.AddMember(t, "member", user.RoleMember)
	rec = f.Do(t, http.MethodGet, "/objectstores/files/metrics", nil, memberKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read metrics as a member: %d %s", rec.Code, rec.Body.String())
	}
	var series struct {
		Window     string           `json:"window"`
		Samples    []map[string]any `json:"samples"`
		Collecting bool             `json:"collecting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &series); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if series.Window != "1h" {
		t.Errorf("no window asked for resolved to %q, want the default", series.Window)
	}
	if series.Samples == nil {
		t.Error("samples came back null, which is a different bug in every client")
	}
	// This store never came up — there is no Docker in this test — so
	// there is nothing to sample, which is a different sentence from
	// "nothing has been sampled yet" and the only one worth showing.
	if series.Collecting {
		t.Error("a store with no container reported that it is being collected")
	}

	if rec = f.Do(t, http.MethodGet, "/objectstores/files/metrics?window=6h", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Errorf("a window this release offers: %d %s", rec.Code, rec.Body.String())
	}
	if rec = f.Do(t, http.MethodGet, "/objectstores/offsite/metrics", nil, f.AdminKey); rec.Code != http.StatusConflict {
		t.Errorf("metrics for a store that runs somewhere else: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if rec = f.Do(t, http.MethodGet, "/objectstores/nope/metrics", nil, f.AdminKey); rec.Code != http.StatusNotFound {
		t.Errorf("metrics for an unknown store: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

// A linked store has no container here, and every operation that means
// one has to say so rather than failing somewhere further in.
func TestALinkedStoreHasNoContainerToOperateOn(t *testing.T) {
	f, _ := withFake(t)
	link(t, f, "offsite", nil)

	for _, c := range []struct {
		method, path string
	}{
		{http.MethodPost, "/objectstores/offsite/start"},
		{http.MethodPost, "/objectstores/offsite/stop"},
		{http.MethodDelete, "/objectstores/offsite/expose"},
	} {
		rec := f.Do(t, c.method, c.path, map[string]any{}, f.AdminKey)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s %s: %d, want 409", c.method, c.path, rec.Code)
		}
	}
	// And its connection is not this instance's to re-point either way
	// round: a managed store refuses a credential.
	rec := f.Do(t, http.MethodPatch, "/objectstores/offsite",
		map[string]any{"description": "somewhere else"}, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Errorf("describing a linked store: %d %s", rec.Code, rec.Body.String())
	}
}

func get(t *testing.T, f *servertest.Fixture, path string, out any) {
	t.Helper()
	rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// itoa renders an id that arrived as JSON, which makes it a float.
func itoa(v any) string {
	n, _ := v.(float64)
	return strconv.FormatInt(int64(n), 10)
}

// -- attachments -----------------------------------------------------

func createApp(t *testing.T, f *servertest.Fixture, project, env, name string) {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/apps", map[string]any{
		"name": name, "project": project, "environment": env,
		"source": "external", "image": "docker.io/library/nginx",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create app %q: %d %s", name, rec.Code, rec.Body.String())
	}
}

func attach(t *testing.T, f *servertest.Fixture, store string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return f.Do(t, http.MethodPost, "/objectstores/"+store+"/attachments", body, f.AdminKey)
}

func appEnv(t *testing.T, f *servertest.Fixture, ref string) map[string]envvar.Resolved {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/apps/"+ref+"/env", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("read app env: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Effective []envvar.Resolved `json:"effective"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode env: %v", err)
	}
	byKey := map[string]envvar.Resolved{}
	for _, r := range out.Effective {
		byKey[r.Key] = r
	}
	return byKey
}

// The whole point of attaching: the app is handed what it needs to
// reach the bucket, and nobody copies a key anywhere.
func TestAnAttachedAppReceivesTheBucketsVariables(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)
	createApp(t, f, "web", "production", "api")

	if rec := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "backups",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("attach: %d %s", rec.Code, rec.Body.String())
	}

	env := appEnv(t, f, "web/production/api")
	want := map[string]string{
		"S3_ENDPOINT":          "https://s3.wasabisys.com",
		"S3_BUCKET":            "backups",
		"S3_ACCESS_KEY_ID":     "AKIAEXAMPLE",
		"S3_SECRET_ACCESS_KEY": "s3cr3t-example",
		"S3_PATH_STYLE":        "true",
	}
	for key, value := range want {
		got, ok := env[key]
		if !ok {
			t.Errorf("the app did not receive %s", key)
			continue
		}
		if got.Value != value {
			t.Errorf("%s = %q, want %q", key, got.Value, value)
		}
		// Labelled as a bucket's rather than as a database's, because
		// "where did this come from" is the question this screen exists
		// to answer.
		if got.Source != envvar.SourceObjectStore {
			t.Errorf("%s came from %q, want %q", key, got.Source, envvar.SourceObjectStore)
		}
	}
	// The variables an app is offered are named on the store, and the
	// values are not: one of them is the secret.
	var store map[string]any
	get(t, f, "/objectstores/offsite", &store)
	attachments, _ := store["attachments"].([]any)
	if len(attachments) != 1 {
		t.Fatalf("the store reports %d attachments, want 1", len(attachments))
	}
	if body := bodyOf(t, f, "/objectstores/offsite"); strings.Contains(body, "s3cr3t-example") {
		t.Error("the secret key came back in the store's own response")
	}

	if rec := f.Do(t, http.MethodDelete,
		"/objectstores/offsite/attachments/web/production/api?bucket=backups", nil, f.AdminKey); rec.Code != http.StatusOK {
		t.Fatalf("detach: %d %s", rec.Code, rec.Body.String())
	}
	if _, still := appEnv(t, f, "web/production/api")["S3_ENDPOINT"]; still {
		t.Error("the app still inherits S3_ENDPOINT after being detached")
	}
}

// Two buckets on one app are one variable with two values unless one of
// them is prefixed — and the refusal names the variables rather than
// the prefix, because an app may hold several and be colliding with
// exactly one.
func TestTwoBucketsOnOneAppNeedAPrefix(t *testing.T) {
	f, _ := withFake(t, "uploads", "backups")
	link(t, f, "offsite", nil)
	createApp(t, f, "web", "production", "api")

	if rec := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "uploads",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("first attach: %d %s", rec.Code, rec.Body.String())
	}

	clash := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "backups",
	})
	if clash.Code != http.StatusConflict {
		t.Fatalf("second bucket at the same prefix: %d %s, want 409", clash.Code, clash.Body.String())
	}
	if body := clash.Body.String(); !strings.Contains(body, "S3_ENDPOINT") {
		t.Errorf("the refusal does not name the variables that collide: %q", body)
	}

	if rec := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "backups", "prefix": "BACKUPS_",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("second bucket with a prefix: %d %s", rec.Code, rec.Body.String())
	}
	env := appEnv(t, f, "web/production/api")
	if env["S3_BUCKET"].Value != "uploads" {
		t.Errorf("S3_BUCKET = %q, want uploads", env["S3_BUCKET"].Value)
	}
	if env["BACKUPS_S3_BUCKET"].Value != "backups" {
		t.Errorf("BACKUPS_S3_BUCKET = %q, want backups", env["BACKUPS_S3_BUCKET"].Value)
	}

	// The same app pointed at the same bucket twice is one set of
	// variables written twice, whatever the prefix says.
	again := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "uploads", "prefix": "OTHER_",
	})
	if again.Code != http.StatusConflict {
		t.Errorf("the same bucket attached twice: %d, want 409", again.Code)
	}
}

// The lesson the datastores learned the hard way, kept from being
// relearned here: a database and a bucket on one app do not compete for
// a name, so neither needs a prefix to sit beside the other.
func TestABucketAndADatabaseDoNotCollideOnOneApp(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)
	createApp(t, f, "web", "production", "api")

	rec := f.Do(t, http.MethodPost, "/datastores", map[string]any{
		"name": "pg", "engine": "postgres",
	}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create datastore: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.Do(t, http.MethodPost, "/datastores/pg/attachments",
		map[string]any{"app": "web/production/api"}, f.AdminKey); rec.Code != http.StatusCreated {
		t.Fatalf("attach datastore: %d %s", rec.Code, rec.Body.String())
	}
	if rec := attach(t, f, "offsite", map[string]any{
		"app": "web/production/api", "bucket": "backups",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("attach bucket beside a database: %d %s", rec.Code, rec.Body.String())
	}

	env := appEnv(t, f, "web/production/api")
	if env["DATABASE_URL"].Source != envvar.SourceDatastore {
		t.Errorf("DATABASE_URL came from %q", env["DATABASE_URL"].Source)
	}
	if env["S3_ENDPOINT"].Source != envvar.SourceObjectStore {
		t.Errorf("S3_ENDPOINT came from %q", env["S3_ENDPOINT"].Source)
	}
}

// Attaching hands an app the store's keys, so it is an admin's from
// both ends.
func TestAMemberCannotAttachAnAppToABucket(t *testing.T) {
	f, _ := withFake(t, "backups")
	link(t, f, "offsite", nil)
	createApp(t, f, "web", "production", "api")
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	rec := f.Do(t, http.MethodPost, "/objectstores/offsite/attachments",
		map[string]any{"app": "web/production/api", "bucket": "backups"}, memberKey)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a member attached an app to a bucket: %d %s", rec.Code, rec.Body.String())
	}
}

// A store pinned to one bucket cannot be attached to another, for the
// same reason it cannot be browsed into one: the refusal is a sentence
// here rather than an access-denied from the provider later.
func TestAPinnedStoreOnlyAttachesItsOwnBucket(t *testing.T) {
	f, _ := withFake(t, "backups", "somebody-elses")
	linkScoped(t, f, "scoped", "backups")
	createApp(t, f, "web", "production", "api")

	if rec := attach(t, f, "scoped", map[string]any{
		"app": "web/production/api", "bucket": "somebody-elses",
	}); rec.Code != http.StatusBadRequest {
		t.Errorf("attached to a bucket the store is not: %d", rec.Code)
	}
	if rec := attach(t, f, "scoped", map[string]any{
		"app": "web/production/api", "bucket": "backups",
	}); rec.Code != http.StatusCreated {
		t.Errorf("could not attach to the store's own bucket: %d %s", rec.Code, rec.Body.String())
	}
}

// bodyOf is a GET's body, for a test asserting on what is *not* in it.
func bodyOf(t *testing.T, f *servertest.Fixture, path string) string {
	t.Helper()
	return f.Do(t, http.MethodGet, path, nil, f.AdminKey).Body.String()
}

// **A linked store has no container here to cap**, and asking is
// refused rather than stored and ignored. The mirror of a managed store
// refusing a credential: each kind refuses the setting that belongs to
// the other, instead of showing a number on a screen that does nothing.
func TestALinkedStoreRefusesALimit(t *testing.T) {
	f, _ := withFake(t)
	link(t, f, "offsite", nil)

	rec := f.Do(t, http.MethodPatch, "/objectstores/offsite",
		map[string]any{"limits": map[string]any{"cpu": 1, "memory_bytes": 512 << 20}}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "container") {
		t.Errorf("the refusal is %q, and it has to say why there is nothing to cap", rec.Body.String())
	}
}

// A managed store takes one, and reports it back — it is what its
// container is created under.
func TestAManagedStoreTakesALimit(t *testing.T) {
	f, _ := withFake(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/objectstores",
		map[string]any{"kind": "managed", "name": "files"}, f.AdminKey), http.StatusCreated)

	var out struct {
		Limits struct {
			CPU    float64 `json:"cpu"`
			Memory int64   `json:"memory_bytes"`
		} `json:"limits"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/objectstores/files",
		map[string]any{"limits": map[string]any{"cpu": 0.5, "memory_bytes": 512 << 20}},
		f.AdminKey, &out), http.StatusOK)
	if out.Limits.CPU != 0.5 || out.Limits.Memory != 512<<20 {
		t.Errorf("it reports %+v", out.Limits)
	}

	// And a number the Engine would refuse is refused here.
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/objectstores/files",
		map[string]any{"limits": map[string]any{"memory_bytes": 1 << 20}}, f.AdminKey),
		http.StatusBadRequest)
}
