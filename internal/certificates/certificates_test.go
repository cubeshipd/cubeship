package certificates_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cubeship/internal/certificates"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"
)

func report(t *testing.T, f *servertest.Fixture, key string) certificates.Report {
	t.Helper()
	rec := f.Do(t, http.MethodGet, "/certificates", nil, key)
	servertest.RequireStatus(t, rec, http.StatusOK)

	var got certificates.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode the report: %v", err)
	}
	return got
}

// How the instance is wired is the operator's business. A member sees an
// app's domains on the app; whether a certificate was issued for one is
// not part of that.
func TestOnlyAnAdminReadsTheCertificates(t *testing.T) {
	f := servertest.New(t)
	_, memberKey := f.AddMember(t, "member", user.RoleMember)

	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/certificates", nil, memberKey),
		http.StatusForbidden)
	servertest.RequireStatus(t, f.Do(t, http.MethodGet, "/certificates", nil, f.AdminKey),
		http.StatusOK)
}

// An instance with no store has issued nothing, and says so with an
// empty report rather than an error — that is the state a fresh install
// is in, and it is on the page for a reason.
func TestAnInstanceThatHasIssuedNothing(t *testing.T) {
	f := servertest.New(t)

	got := report(t, f, f.AdminKey)
	if len(got.Certificates) != 0 {
		t.Errorf("certificates: %+v", got.Certificates)
	}
	if !got.TLSEnabled {
		t.Error("TLS is reported as impossible on an instance with a domain")
	}
	// The names are known and nothing has been issued for them yet,
	// which is what pending means.
	if len(got.Missing) == 0 {
		t.Fatal("nothing is reported as missing a certificate")
	}
	for _, m := range got.Missing {
		if m.Reason != certificates.ReasonPending {
			t.Errorf("%s is %q, want %q", m.Host, m.Reason, certificates.ReasonPending)
		}
	}
}

// Without a domain Traefik is started with no resolver at all: it asks
// for nothing, so no name is waiting on anything. The instance is the
// answer, not the name.
func TestWithNoDomainNothingIsPending(t *testing.T) {
	f := servertest.NewUnconfigured(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, "gateway.example.com")

	got := report(t, f, f.AdminKey)
	if got.TLSEnabled {
		t.Error("TLS is reported as possible with no domain")
	}
	if len(got.Missing) != 1 || got.Missing[0].Reason != certificates.ReasonNoTLS {
		t.Errorf("missing is %+v", got.Missing)
	}
}

// The instance's own two names are on the page. They are the ones an
// operator notices first when HTTPS stops working, and neither belongs
// to an app.
func TestTheInstancesOwnNamesAreReported(t *testing.T) {
	f := servertest.New(t)

	want := map[string]bool{
		servertest.APIHost:      false,
		servertest.RegistryHost: false,
	}
	for _, m := range report(t, f, f.AdminKey).Missing {
		if _, ok := want[m.Host]; ok {
			if !m.Instance {
				t.Errorf("%s is not marked as the instance's own", m.Host)
			}
			want[m.Host] = true
		}
	}
	for host, found := range want {
		if !found {
			t.Errorf("%s is not in the report", host)
		}
	}
}

// A name added to an app that has not been deployed since is a name
// Traefik has never been told about: a container keeps the labels it was
// created with. That is the commonest reason a certificate is missing,
// and the answer to it is "redeploy" rather than anything about DNS.
func TestANameAddedAndNotDeployedSaysSo(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, "gateway.example.com")

	var found *certificates.Missing
	for _, m := range report(t, f, f.AdminKey).Missing {
		if m.Host == "gateway.example.com" {
			found = &m
			break
		}
	}
	if found == nil {
		t.Fatal("the app's name is not reported as missing a certificate")
	}
	if found.App != created.Reference {
		t.Errorf("it is attributed to %q, want %q", found.App, created.Reference)
	}
	if found.Reason != certificates.ReasonNotDeployed {
		t.Errorf("the reason is %q, want %q", found.Reason, certificates.ReasonNotDeployed)
	}
}

// storeWith writes an acme.json holding one certificate for each name,
// the way Traefik does. It generates them here rather than carrying a
// fixture: a certificate in a repository is one that expires.
func storeWith(t *testing.T, dataDir string, hosts ...string) {
	t.Helper()

	entries := make([]map[string]any, 0, len(hosts))
	for _, host := range hosts {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: host},
			DNSNames:     []string{host},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(60 * 24 * time.Hour),
		}
		der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, map[string]any{
			"domain": map[string]any{"main": host},
			"certificate": base64.StdEncoding.EncodeToString(
				pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		})
	}

	path := certificates.StorePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"letsencrypt": map[string]any{
			"Account":      map[string]any{"Email": "ops@example.com"},
			"Certificates": entries,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The whole path, from the file Traefik writes to what a page renders:
// a certificate is found, attributed to the app served at that name, and
// one for a name nothing serves any more is called what it is.
func TestTheReportReadsTheStoreAndAttributesEachCertificate(t *testing.T) {
	f := servertest.New(t)

	var created struct {
		Reference string `json:"reference"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/apps", map[string]string{
		"name": "gateway", "project": "web",
	}, f.AdminKey, &created), http.StatusCreated)
	servertest.AddDomain(t, f, f.AdminKey, created.Reference, "gateway.example.com")

	storeWith(t, f.DataDir, "gateway.example.com", "deleted.example.com", servertest.APIHost)

	got := report(t, f, f.AdminKey)
	if got.ACMEEmail != "ops@example.com" {
		t.Errorf("the account email is %q", got.ACMEEmail)
	}
	if len(got.Certificates) != 3 {
		t.Fatalf("read %d certificates, want 3", len(got.Certificates))
	}

	byHost := map[string]certificates.Certificate{}
	for _, c := range got.Certificates {
		byHost[c.Host] = c
	}
	if got := byHost["gateway.example.com"]; got.App != created.Reference || got.Orphan {
		t.Errorf("the app's certificate came out %+v", got)
	}
	if got := byHost[servertest.APIHost]; !got.Instance || got.Orphan {
		t.Errorf("the instance's certificate came out %+v", got)
	}
	// An app deleted after its certificate was issued leaves it behind:
	// still valid, still in the file, serving nothing.
	if got := byHost["deleted.example.com"]; !got.Orphan {
		t.Errorf("a certificate nothing serves came out %+v", got)
	}

	// And the name that now has one is no longer reported as missing.
	for _, m := range got.Missing {
		if m.Host == "gateway.example.com" {
			t.Errorf("a name with a certificate is still reported missing one: %+v", m)
		}
	}
}
