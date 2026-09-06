package credential_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

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
// used by two different things at once. It carries no provider of its
// own, so nothing about storing it decides which of those jobs it may
// ever do — which matters because most API secrets can only be read at
// the moment they are issued.
func TestOneCredentialServesTwoDifferentUses(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	aws := create(t, f, map[string]any{
		"label": "Company AWS", "username": "AKIAEXAMPLE", "password": "secret",
	})
	id := int64(aws["id"].(float64))

	// Route 53 writes records with it.
	rec := f.Do(t, http.MethodPost, "/dns",
		map[string]any{"provider": "aws", "credential_id": id}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("connect DNS: %d %s", rec.Code, rec.Body.String())
	}

	// And a registry logs in with the same row. Under the old model
	// this was impossible without storing the key twice.
	rec = f.Do(t, http.MethodPost, "/registries",
		map[string]any{"provider": "generic", "credential_id": id, "host": "ghcr.io"}, f.AdminKey)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create registry: %d %s", rec.Code, rec.Body.String())
	}

	// One row, so rotating it is one edit — and the listing says both
	// things are standing on it.
	rec = f.Do(t, http.MethodGet, "/credentials", nil, f.AdminKey)
	var out []struct {
		Label   string   `json:"label"`
		InUseBy []string `json:"in_use_by"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("the instance holds %d credentials, want 1", len(out))
	}
	uses := strings.Join(out[0].InUseBy, ", ")
	for _, want := range []string{"ghcr.io", "Amazon Web Services"} {
		if !strings.Contains(uses, want) {
			t.Errorf("in_use_by is %q, missing %q", uses, want)
		}
	}
}

// The secret goes in and never comes back. There is no endpoint that
// could return one: the daemon is what talks to the provider.
func TestTheSecretIsNeverReturned(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	created := create(t, f, map[string]any{"label": "Cloudflare", "password": "cf-token-hunter2"})
	if _, leaked := created["password"]; leaked {
		t.Error("create returned the secret")
	}
	rec := f.Do(t, http.MethodGet, "/credentials", nil, f.AdminKey)
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("the listing carries the secret: %s", rec.Body.String())
	}
	// The key id is not a secret — it is the half you read off a
	// console — so a credential that has one still reports it.
	aws := create(t, f, map[string]any{
		"label": "AWS", "username": "AKIAEXAMPLE", "password": "s",
	})
	if aws["username"] != "AKIAEXAMPLE" {
		t.Errorf("the key id came back as %v", aws["username"])
	}
}

// A label and a secret, and nothing else is asked. A bare token with no
// first half is a normal credential, not a half-filled one: whether a
// login has two halves is the use's question, not this module's.
func TestWhatACredentialAsksFor(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)

	create(t, f, map[string]any{"label": "A bare token", "password": "cf-token"})

	refused := []map[string]any{
		// No secret at all — it would reach nothing.
		{"label": "c"},
		// No label — there would be nothing to call it by.
		{"label": "", "password": "s"},
	}
	for _, body := range refused {
		rec := f.Do(t, http.MethodPost, "/credentials", body, f.AdminKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("create %v: %d %s, want 400", body, rec.Code, rec.Body.String())
		}
	}

	create(t, f, map[string]any{"label": "Taken", "password": "s"})
	rec := f.Do(t, http.MethodPost, "/credentials",
		map[string]any{"label": "taken", "username": "k", "password": "s"}, f.AdminKey)
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
		"label": "Docker Hub", "username": "someone", "password": "hub-token",
	})
	id := int64(cred["id"].(float64))

	rec := f.Do(t, http.MethodPost, "/registries",
		map[string]any{"provider": "generic", "credential_id": id, "host": "docker.io"}, f.AdminKey)
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

// Everything about credentials is an admin's, reading included: the
// list is the map of what this instance can reach.
func TestCredentialsAreEntirelyAnAdmins(t *testing.T) {
	dbtest.RequireDatabase(t)
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/credentials"},
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

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
