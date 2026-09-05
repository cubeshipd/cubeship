package app_test

import (
	"net/http"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
)

// Traefik routes by host and nothing else. The unique index catches two
// apps claiming one name, but it knows nothing about the routers the
// daemon gives itself — so an app taking the instance's own name would
// be created happily, and after its next deploy either the dashboard or
// the registry would stop answering, decided by label ordering.
func TestAnAppCannotClaimTheInstancesOwnNames(t *testing.T) {
	f := servertest.New(t)
	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "myapp", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	for _, host := range []string{
		servertest.Domain,
		strings.ToUpper(servertest.Domain),
		servertest.Domain + ".",
		servertest.RegistryHost,
	} {
		rec := f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
			map[string]any{"host": host}, f.AdminKey)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q was accepted (%d %s)", host, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}

	// A name that is not a DNS name at all, including one that would
	// close the Traefik rule it is interpolated into and write another.
	for _, host := range []string{
		"not a host",
		"a`)||Host(`anything.example.com",
	} {
		servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
			map[string]any{"host": host}, f.AdminKey), http.StatusBadRequest)
	}

	// An ordinary name still works.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": "myapp.example.com"}, f.AdminKey), http.StatusCreated)
}

// The suggestion reaches the client, because the client is where
// somebody decides to use it. It is only ever offered: an app is still
// created with no domain.
func TestAnAppReportsAHostItCouldAnswerAt(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference     string `json:"reference"`
		SuggestedHost string `json:"suggested_host"`
		Domains       []struct {
			Host string `json:"host"`
		} `json:"domains"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)

	want := "gateway.production.web." + servertest.Domain
	if created.SuggestedHost != want {
		t.Errorf("suggested %q, want %q", created.SuggestedHost, want)
	}
	if len(created.Domains) != 0 {
		t.Errorf("a suggestion was taken for an assignment: %v", created.Domains)
	}

	// And it is a name the daemon accepts, which is the whole point of
	// offering it.
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+created.Reference+"/domains",
		map[string]any{"host": created.SuggestedHost}, f.AdminKey), http.StatusCreated)
}

// An instance with no domain has nothing to build a name under, and says
// so by leaving it out rather than by offering half of one.
func TestNoDomainOnTheInstanceMeansNoSuggestion(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		SuggestedHost string `json:"suggested_host"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)
	if created.SuggestedHost != "" {
		t.Errorf("suggested %q with no instance domain", created.SuggestedHost)
	}
}
