package extregistry_test

import (
	"cubeship/internal/user"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"cubeship/internal/extregistry"
	"cubeship/internal/server/servertest"
)

const orgPath = "/registries"

type credential struct {
	ID        int64  `json:"id"`
	Provider  string `json:"provider"`
	Host      string `json:"host"`
	Namespace string `json:"namespace"`
	Region    string `json:"region"`
	Username  string `json:"username"`
}

// A password is stored so it can be sent to a registry, which means an
// endpoint that hands it back turns every read into a way out for it.
// Nothing returns one.
func TestThePasswordIsNeverReturned(t *testing.T) {
	f := servertest.New(t)

	const secret = "dop_v1_verysecrettoken"
	rec := f.Do(t, http.MethodPost, orgPath, map[string]string{
		"provider": "digitalocean", "namespace": "acme",
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
		"provider": "generic", "host": "ghcr.io", "username": "a", "password": "b",
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, orgPath, body, f.AdminKey), http.StatusCreated)

	// The registry is the identity, so the same one twice is a conflict
	// however it is spelled.
	body["host"] = "https://ghcr.io/"
	rec := f.Do(t, http.MethodPost, orgPath, body, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)

	// A different registry is not.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, orgPath, map[string]string{
		"provider": "generic", "host": "quay.io", "username": "a", "password": "b",
	}, f.AdminKey), http.StatusCreated)
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
		{"127.0.0.1:5000/web/production/api", "127.0.0.1:5000"},
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
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	for _, call := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPost, map[string]string{
			"provider": "generic", "host": "ghcr.io", "username": "a", "password": "b",
		}},
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
		"provider": "generic", "host": "ghcr.io", "username": "old", "password": "old-token",
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

// A registry login is a password this instance will send somewhere, so
// reading the list is an admin's. A member deploys apps that use one and
// never sees it.
func TestAMemberCannotReadTheCredentials(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := servertest.CreateUser(t, f.DB, "member", user.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, orgPath, nil, memberKey), http.StatusForbidden)
}

// DigitalOcean's host never varies — what differs between accounts is
// the first path segment. Asking for a URL would be asking someone to
// retype a constant and get the rest wrong.
func TestDigitalOceanIsAskedForItsNameAndNotItsURL(t *testing.T) {
	f := servertest.New(t)

	var created credential
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, orgPath, map[string]string{
		"provider": "digitalocean", "namespace": "acme",
		"username": "someone@example.com", "password": "dop_v1_token",
	}, f.AdminKey, &created), http.StatusCreated)

	if created.Host != extregistry.DigitalOceanHost {
		t.Errorf("host is %q, want the fixed DigitalOcean one", created.Host)
	}
	if created.Namespace != "acme" {
		t.Errorf("namespace is %q", created.Namespace)
	}

	// And it is required: without it there is no image path to build.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, orgPath, map[string]string{
		"provider": "digitalocean", "username": "a", "password": "b",
	}, f.AdminKey), http.StatusBadRequest)
}

// An ECR registry lives in a region and its host carries an account id.
// The account id is discovered; the region cannot be, so it is refused
// rather than guessed.
func TestAWSNeedsARegion(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPost, orgPath, map[string]string{
		"provider": "aws", "username": "AKIAEXAMPLE", "password": "secret",
	}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "region") {
		t.Errorf("the refusal does not name what is missing: %q", rec.Body.String())
	}
}

// A provider the daemon cannot act on is refused at creation, the same
// way an app's source is: accepting one would store a credential no pull
// could ever use.
func TestAnUnknownProviderIsRefused(t *testing.T) {
	f := servertest.New(t)

	for _, provider := range []string{"", "gcp", "GENERIC", "azure"} {
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, orgPath, map[string]string{
			"provider": provider, "host": "example.com", "username": "a", "password": "b",
		}, f.AdminKey), http.StatusBadRequest)
	}
}
