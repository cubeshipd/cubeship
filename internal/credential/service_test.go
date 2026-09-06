package credential_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/credential"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

func create(t *testing.T, f *servertest.Fixture, body map[string]any) map[string]any {
	t.Helper()
	rec := f.Do(t, http.MethodPost, "/credentials", body, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create credential: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// The point of the module, end to end: one AWS key is stored once and
// offered wherever it can be used. Before this it was two rows — a
// "route53" DNS provider and an "aws" registry login — holding the same
// secret, rotated in two places or in one and forgotten in the other.
func TestOneKeyIsOfferedToEveryJobItCanDo(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	aws := create(t, f, map[string]any{
		"provider": "aws", "label": "Company AWS",
		"username": "AKIAEXAMPLE", "password": "secret",
	})
	create(t, f, map[string]any{
		"provider": "cloudflare", "label": "Cloudflare", "password": "cf-token",
	})
	create(t, f, map[string]any{
		"provider": "generic", "label": "Docker Hub",
		"username": "someone", "password": "hub-token",
	})

	labels := func(capability string) []string {
		t.Helper()
		path := "/credentials"
		if capability != "" {
			path += "?capability=" + capability
		}
		rec := f.Do(t, http.MethodGet, path, nil, f.AdminKey)
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", path, rec.Code, rec.Body.String())
		}
		var out []struct {
			Label string `json:"label"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		var names []string
		for _, c := range out {
			names = append(names, c.Label)
		}
		return names
	}

	if got := len(labels("")); got != 3 {
		t.Errorf("the instance holds %d credentials, want 3", got)
	}

	// The AWS key appears under both jobs. That is the whole feature.
	dns := labels("dns")
	if len(dns) != 2 || !has(dns, "Company AWS") || !has(dns, "Cloudflare") {
		t.Errorf("the DNS page would offer %v, want the AWS key and Cloudflare", dns)
	}
	registry := labels("registry")
	if len(registry) != 2 || !has(registry, "Company AWS") || !has(registry, "Docker Hub") {
		t.Errorf("the registry page would offer %v, want the AWS key and Docker Hub", registry)
	}

	// And it is one row, so rotating it is one edit.
	if _, ok := aws["id"]; !ok {
		t.Fatal("the created credential has no id")
	}
}

// The secret goes in and never comes back. There is no endpoint that
// could return one: the daemon is what talks to the provider.
func TestTheSecretIsNeverReturned(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	created := create(t, f, map[string]any{
		"provider": "cloudflare", "label": "Cloudflare", "password": "cf-token-hunter2",
	})
	if _, leaked := created["password"]; leaked {
		t.Error("create returned the secret")
	}
	rec := f.Do(t, http.MethodGet, "/credentials", nil, f.AdminKey)
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("the listing carries the secret: %s", rec.Body.String())
	}
	// The key id is not a secret — it is the half you read off a
	// console — so a provider that has one still reports it.
	aws := create(t, f, map[string]any{
		"provider": "aws", "label": "AWS", "username": "AKIAEXAMPLE", "password": "s",
	})
	if aws["username"] != "AKIAEXAMPLE" {
		t.Errorf("the key id came back as %v", aws["username"])
	}
}

// Both halves or neither, and the refusals are symmetrical: silently
// dropping the extra is how a credential comes out different from what
// somebody thought they stored.
func TestWhatEachProviderAsksFor(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	refused := []map[string]any{
		// A key with no id.
		{"provider": "aws", "label": "a", "password": "s"},
		// A token with a name beside it.
		{"provider": "cloudflare", "label": "b", "username": "who", "password": "s"},
		// No secret at all.
		{"provider": "cloudflare", "label": "c"},
		// No label — there would be nothing to call it by.
		{"provider": "cloudflare", "label": "", "password": "s"},
		// A provider this release cannot act through.
		{"provider": "hetzner", "label": "d", "password": "s"},
	}
	for _, body := range refused {
		rec := f.Do(t, http.MethodPost, "/credentials", body, f.AdminKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %v: %d %s, want 400", body, rec.Code, rec.Body.String())
		}
	}

	create(t, f, map[string]any{"provider": "cloudflare", "label": "Taken", "password": "s"})
	rec := f.Do(t, http.MethodPost, "/credentials",
		map[string]any{"provider": "aws", "label": "taken", "username": "k", "password": "s"}, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Errorf("a label that differs only in case: %d %s, want 409", rec.Code, rec.Body.String())
	}
}

// Deleting one that something authenticates with is refused, and the
// refusal names it. Cascading would leave a registry that cannot log
// in, and the way that surfaces is a deploy failing with nobody
// watching.
func TestDeletingOneInUseIsRefusedAndSaysByWhat(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	cred := create(t, f, map[string]any{
		"provider": "generic", "label": "Docker Hub",
		"username": "someone", "password": "hub-token",
	})
	id := int64(cred["id"].(float64))

	rec := f.Do(t, http.MethodPost, "/registries",
		map[string]any{"credential_id": id, "host": "docker.io"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create registry: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.Do(t, http.MethodDelete, "/credentials/"+itoa(id), nil, f.AdminKey)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in use: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "docker.io") {
		t.Errorf("the refusal does not name what is using it: %s", rec.Body.String())
	}

	// The listing says so too, before anybody tries.
	rec = f.Do(t, http.MethodGet, "/credentials", nil, f.AdminKey)
	if !strings.Contains(rec.Body.String(), "docker.io") {
		t.Errorf("the listing does not say what is using it: %s", rec.Body.String())
	}
}

// A registry cannot authenticate as an account that does not do
// registries. Refused when it is stored, not discovered at a deploy.
func TestARegistryRefusesAnAccountThatCannotDoRegistries(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	cred := create(t, f, map[string]any{
		"provider": "cloudflare", "label": "Cloudflare", "password": "cf-token",
	})
	rec := f.Do(t, http.MethodPost, "/registries",
		map[string]any{"credential_id": int64(cred["id"].(float64)), "host": "docker.io"}, f.AdminKey)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a Cloudflare token was accepted for a registry: %d %s", rec.Code, rec.Body.String())
	}
}

// Everything about credentials is an admin's, reading included: the
// list is the map of what this instance can reach.
func TestCredentialsAreEntirelyAnAdmins(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/credentials"},
		{http.MethodGet, "/credentials/providers"},
		{http.MethodPost, "/credentials"},
		{http.MethodPatch, "/credentials/1"},
		{http.MethodDelete, "/credentials/1"},
	} {
		rec := f.Do(t, c.method, c.path, map[string]any{}, memberKey)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member: %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

// The providers a form is built from come from the daemon, so a client
// never keeps its own copy of what this release supports.
func TestTheProvidersAreServedRatherThanGuessed(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	rec := f.Do(t, http.MethodGet, "/credentials/providers", nil, f.AdminKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("list providers: %d %s", rec.Code, rec.Body.String())
	}
	var out []struct {
		Provider      string   `json:"provider"`
		Capabilities  []string `json:"capabilities"`
		UsernameLabel string   `json:"username_label"`
		PasswordLabel string   `json:"password_label"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(credential.Providers()) {
		t.Fatalf("served %d providers, the daemon has %d", len(out), len(credential.Providers()))
	}
	for _, p := range out {
		if p.PasswordLabel == "" || len(p.Capabilities) == 0 {
			t.Errorf("%s arrives unusable for building a form: %+v", p.Provider, p)
		}
	}
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
