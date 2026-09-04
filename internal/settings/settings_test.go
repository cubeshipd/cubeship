package settings_test

import (
	"net/http"
	"testing"

	"cubeship/internal/org"
	"cubeship/internal/server/servertest"
	"cubeship/internal/settings"
)

type response struct {
	Domain       string `json:"domain"`
	ACMEEmail    string `json:"acme_email"`
	RegistryHost string `json:"registry_host"`
	TLSEnabled   bool   `json:"tls_enabled"`
}

func read(t *testing.T, f *servertest.Fixture, key string) response {
	t.Helper()
	var got response
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/settings", nil, key, &got), http.StatusOK)
	return got
}

// The state a fresh install is in: no domain, no contact address, and a
// daemon that runs anyway. Everything else here is about leaving it.
func TestUnconfiguredInstance(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	got := read(t, f, f.AdminKey)
	if got.Domain != "" || got.ACMEEmail != "" {
		t.Fatalf("a fresh instance is configured: %+v", got)
	}
	if got.RegistryHost != "" {
		t.Error("a registry host was reported with no domain to derive it from")
	}
	if got.TLSEnabled {
		t.Error("TLS is claimed with no contact address")
	}
}

// Apps are usable before a domain exists — that is the whole point of
// deferring configuration. Only the push path is missing.
func TestAppsCanBeCreatedBeforeADomainExists(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
		Image     string `json:"image"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	if created.Reference != "acme/web/production/myapp" {
		t.Fatalf("reference is %q", created.Reference)
	}
	if created.Image != "" {
		t.Errorf("a push path was reported with no registry to push to: %q", created.Image)
	}

	// Deploying is what actually needs the registry, and says so.
	rec := f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/deploy", nil, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusConflict)
}

// The push path is derived, not frozen at creation — so an app created
// before the domain existed gets a correct one the moment it does.
func TestConfiguringADomainGivesExistingAppsAPushPath(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
		Image     string `json:"image"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "domain": "myapp.example.com", "org": "acme", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"domain": "example.com"}, f.AdminKey), http.StatusOK)

	var after struct {
		Image string `json:"image"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet,
		"/apps/"+created.Reference, nil, f.AdminKey, &after), http.StatusOK)

	if want := "registry.example.com/" + created.Reference; after.Image != want {
		t.Fatalf("push path is %q, want %q", after.Image, want)
	}
}

// Certificates need both: Let's Encrypt will not register an account
// without a contact address, and there is nothing to certify without a
// domain.
func TestTLSNeedsBothADomainAndAContactAddress(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"domain": "example.com"}, f.AdminKey), http.StatusOK)
	if read(t, f, f.AdminKey).TLSEnabled {
		t.Error("TLS is claimed with a domain but no contact address")
	}

	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"acme_email": "admin@example.com"}, f.AdminKey), http.StatusOK)

	got := read(t, f, f.AdminKey)
	if !got.TLSEnabled {
		t.Error("TLS is still off with both configured")
	}
	// Setting the email must not have cleared the domain: only the field
	// sent is changed.
	if got.Domain != "example.com" {
		t.Errorf("the domain was lost when the contact address was set: %+v", got)
	}
}

// Instance configuration is the VPS operator's. An organization admin
// runs their own org, not the machine.
func TestOnlyASuperAdminChangesSettings(t *testing.T) {
	f := servertest.New(t)
	_, adminKey := f.AddMember(t, "org-admin", org.RoleAdmin)

	// Reading is fine: a dashboard has to know where to tell you to push.
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/settings", nil, adminKey), http.StatusOK)

	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"domain": "hijacked.example.com"}, adminKey), http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/settings", nil, ""), http.StatusUnauthorized)
}

func TestUnknownSettingIsRejected(t *testing.T) {
	f := servertest.New(t)

	rec := f.Do(t, http.MethodPut, "/settings", map[string]string{}, f.AdminKey)
	servertest.RequireStatus(t, rec, http.StatusBadRequest)
}

// Seeding is how an install upgrading from the release where these were
// environment variables keeps its configuration — without overwriting
// what an operator has since changed.
func TestSeedingNeverOverwrites(t *testing.T) {
	f := servertest.NewUnconfigured(t)
	ctx := t.Context()

	if err := f.Server.Settings.SeedFromEnv(ctx, map[string]string{settings.Domain: "from-env.example.com"}); err != nil {
		t.Fatalf("SeedFromEnv: %v", err)
	}
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings",
		map[string]string{"domain": "chosen.example.com"}, f.AdminKey), http.StatusOK)

	if err := f.Server.Settings.SeedFromEnv(ctx, map[string]string{settings.Domain: "from-env.example.com"}); err != nil {
		t.Fatalf("SeedFromEnv: %v", err)
	}
	if got := read(t, f, f.AdminKey).Domain; got != "chosen.example.com" {
		t.Fatalf("the environment overwrote what the operator chose: %q", got)
	}
}
