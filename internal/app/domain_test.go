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
