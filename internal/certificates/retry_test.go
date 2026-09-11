package certificates_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"cubeship/internal/certificates"
	"cubeship/internal/platform/traefik"
	"cubeship/internal/server/servertest"
)

func asked(t *testing.T, dir string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, "traefik-dynamic", traefik.RetryFileName))
	return err == nil
}

func retrier(f *servertest.Fixture) *certificates.Retrier {
	return &certificates.Retrier{Certs: f.Server.Certs, DataDir: f.DataDir}
}

// The whole point: a name Traefik knows about and has no certificate for
// is asked about again. Nothing else ever asks — Traefik resolves when
// its configuration changes and at no other moment — so without this the
// first attempt for a name is the only one it gets.
func TestANameWaitingOnACertificateIsAskedAboutAgain(t *testing.T) {
	f := servertest.New(t)

	pending, err := f.Server.Certs.Pending(t.Context())
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("nothing is waiting on a certificate, so there is nothing to retry")
	}

	retrier(f).Once(t.Context())
	if !asked(t, f.DataDir) {
		t.Fatal("an instance missing a certificate did not ask Traefik to try again")
	}

	// And asking twice has to leave two different configurations, or
	// Traefik skips the second as the one it already has.
	retrier(f).Once(t.Context())
	if asked(t, f.DataDir) {
		t.Error("the second ask left the directory exactly as the first did")
	}
}

// Every attempt asks Let's Encrypt about every name that is missing one,
// against a limit of five failed validations per hostname per hour. So
// the names that are not waiting on Traefik must not cause an attempt:
// an instance with no domain has no resolver at all, and asking would
// spend nothing but somebody else's allowance under the same registered
// domain.
func TestNothingIsAskedForWhenNothingIsWaitingOnTraefik(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, "gateway.example.com")

	// The name is missing a certificate, and the reason is the instance
	// rather than the name — which is a thing for a person to do.
	pending, err := f.Server.Certs.Pending(t.Context())
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("an instance with no domain reports names waiting on Traefik: %+v", pending)
	}

	retrier(f).Once(t.Context())
	if asked(t, f.DataDir) {
		t.Error("an instance with no domain asked for a certificate anyway")
	}
}
