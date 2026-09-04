package extregistry_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/extregistry"
	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
)

const orgPath = "/orgs/acme/registries"

type credential struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Username string `json:"username"`
}

// A password is stored so it can be sent to a registry, which means an
// endpoint that hands it back turns every read into a way out for it.
// Nothing returns one.
func TestThePasswordIsNeverReturned(t *testing.T) {
	f := servertest.New(t)

	const secret = "dop_v1_verysecrettoken"
	rec := f.Do(t, http.MethodPost, orgPath, map[string]string{
		"name": "DigitalOcean", "host": "registry.digitalocean.com",
		"username": "someone@example.com", "password": secret,
	}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusCreated)
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("the create response contains the password")
	}

	list := f.Do(t, http.MethodGet, orgPath, nil, f.AdminKey)
	servertest.RequireStatus(t, list, http.StatusOK)
	if strings.Contains(list.Body.String(), secret) {
		t.Fatal("the listing contains the password")
	}
}

// One login per registry per organization: two would make "which one
// does this pull use" a question with no answer.
func TestOneCredentialPerRegistry(t *testing.T) {
	f := servertest.New(t)

	body := map[string]string{
		"name": "DO", "host": "registry.digitalocean.com", "username": "a", "password": "b",
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, orgPath, body, f.AdminKey), http.StatusCreated)

	// Same host under a different name is still the same registry.
	body["name"] = "DO again"
	rec := f.Do(t, http.MethodPost, orgPath, body, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "registry") {
		t.Errorf("the conflict does not say which collision it was: %q", rec.Body.String())
	}

	// And the same name for a different registry is the other conflict.
	rec = f.Do(t, http.MethodPost, orgPath, map[string]string{
		"name": "DO", "host": "ghcr.io", "username": "a", "password": "b",
	}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)
}

// What someone types and what an image reference carries are rarely
// spelled the same. They have to end up comparable, or a login is stored
// that no pull ever finds.
func TestHostsAreNormalized(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"registry.digitalocean.com", "registry.digitalocean.com"},
		{"https://registry.digitalocean.com/", "registry.digitalocean.com"},
		{"http://ghcr.io", "ghcr.io"},
		{"GHCR.IO", "ghcr.io"},
		{"registry.digitalocean.com/acme", "registry.digitalocean.com"},
		// Every spelling of the Hub lands on the one the daemon uses.
		{"docker.io", extregistry.DockerHub},
		{"registry-1.docker.io", extregistry.DockerHub},
		{"hub.docker.com", extregistry.DockerHub},
	} {
		if got := extregistry.NormalizeHost(tt.in); got != tt.want {
			t.Errorf("NormalizeHost(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The host of an image is what a credential is matched on, so the two
// have to agree about references with no registry in them at all.
func TestHostOfAnImage(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"registry.digitalocean.com/acme/api", "registry.digitalocean.com"},
		{"ghcr.io/acme/api", "ghcr.io"},
		{"localhost:5000/api", "localhost:5000"},
		{"127.0.0.1:5000/acme/web/production/api", "127.0.0.1:5000"},
		// No registry in the name means Docker Hub, whether or not the
		// reference has a slash in it.
		{"acme/api", extregistry.DockerHub},
		{"postgres", extregistry.DockerHub},
		{"library/postgres", extregistry.DockerHub},
	} {
		if got := extregistry.HostOf(tt.in); got != tt.want {
			t.Errorf("HostOf(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A credential is a way into somewhere; a member who can deploy is not
// thereby allowed to read or change the login they deploy through.
func TestOnlyAdminsManageCredentials(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", org.RoleMember)

	for _, call := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPost, map[string]string{"name": "x", "host": "ghcr.io", "username": "a", "password": "b"}},
	} {
		rec := f.Do(t, call.method, orgPath, call.body, memberKey)
		servertest.RequireStatus(t, rec, http.StatusForbidden)
	}
}

// Rotation replaces the login and keeps the registry. Re-pointing a
// credential at a different host in place would silently send an app's
// pulls somewhere else.
func TestRotationKeepsTheHost(t *testing.T) {
	f := servertest.New(t)

	var created credential
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, orgPath, map[string]string{
		"name": "GitHub", "host": "ghcr.io", "username": "old", "password": "old-token",
	}, f.AdminKey, &created), http.StatusCreated)

	var updated credential
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPut,
		orgPath+"/"+strconv.FormatInt(created.ID, 10), map[string]string{
			"username": "new", "password": "new-token",
		}, f.AdminKey, &updated), http.StatusOK)

	if updated.Username != "new" {
		t.Errorf("username is %q after rotation", updated.Username)
	}
	if updated.Host != "ghcr.io" {
		t.Errorf("host changed to %q", updated.Host)
	}
}

// Credentials belong to an organization, and an organization's contents
// are invisible to anyone outside it — the same 404 an unknown org gets.
func TestAnOutsiderSeesNothing(t *testing.T) {
	f := servertest.New(t)
	_, outsiderKey := servertest.CreateUser(t, f.DB, "outsider", false)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, orgPath, nil, outsiderKey), http.StatusNotFound)
}
