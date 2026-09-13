package certificates

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// issue makes a certificate the way Let's Encrypt would hand one over,
// so the parser is exercised on the real shape rather than on a fixture
// somebody typed.
func issue(t *testing.T, host string, notAfter time.Time, sans ...string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	// Signed by something else, the way a real one is: the issuer is the
	// CA's name, and this page shows it.
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ca := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "R11"},
		NotBefore:             notAfter.Add(-365 * 24 * time.Hour),
		NotAfter:              notAfter.Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(0x0a0b0c),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     append([]string{host}, sans...),
		NotBefore:    notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	// Traefik stores base64 of the PEM chain, not of the DER.
	return base64.StdEncoding.EncodeToString(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// writeStore lays out an acme.json the way Traefik does: one entry per
// resolver, the account beside the certificates.
func writeStore(t *testing.T, dataDir string, email string, entries ...map[string]any) string {
	t.Helper()
	path := StorePath(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	store := map[string]any{
		"letsencrypt": map[string]any{
			"Account":      map[string]any{"Email": email},
			"Certificates": entries,
		},
	}
	raw, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func entry(host, certificate string) map[string]any {
	return map[string]any{
		"domain":      map[string]any{"main": host},
		"certificate": certificate,
		// The key is in the file beside it. Nothing reads it, and this
		// is here so that stays true by being noticed if it changes.
		"key":   "c2VjcmV0",
		"Store": "default",
	}
}

// The store is Traefik's, and this reads it: what it holds, when each
// one runs out, and who signed it.
func TestReadingTraefiksStore(t *testing.T) {
	dir := t.TempDir()
	expires := time.Now().Add(45 * 24 * time.Hour).UTC().Truncate(time.Second)
	path := writeStore(t, dir, "ops@example.com",
		entry("late.example.com", issue(t, "late.example.com", expires)),
		entry("soon.example.com", issue(t, "soon.example.com", expires.Add(-30*24*time.Hour))))

	email, certs, err := readStore(path)
	if err != nil {
		t.Fatalf("readStore: %v", err)
	}
	if email != "ops@example.com" {
		t.Errorf("account email is %q", email)
	}
	if len(certs) != 2 {
		t.Fatalf("read %d certificates, want 2", len(certs))
	}
	// Soonest first: the table is read for what is about to break.
	if certs[0].Host != "soon.example.com" || certs[1].Host != "late.example.com" {
		t.Errorf("order is %q then %q", certs[0].Host, certs[1].Host)
	}
	if !certs[1].NotAfter.Equal(expires) {
		t.Errorf("expiry is %s, want %s", certs[1].NotAfter, expires)
	}
	if certs[1].Issuer != "R11" {
		t.Errorf("issuer is %q", certs[1].Issuer)
	}
	if certs[1].Serial != "0a:0b:0c" {
		t.Errorf("serial is %q", certs[1].Serial)
	}
	if days := certs[1].DaysLeft(time.Now()); days < 43 || days > 45 {
		t.Errorf("days left is %d, want about 45", days)
	}
}

// The names beside the main one are worth showing, and the main one is
// not repeated among them — it is already the row's title.
func TestTheOtherNamesOnACertificate(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, "",
		entry("api.example.com", issue(t, "api.example.com", time.Now().Add(time.Hour), "admin.example.com")))

	_, certs, err := readStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 || len(certs[0].SANs) != 1 || certs[0].SANs[0] != "admin.example.com" {
		t.Fatalf("read %+v", certs)
	}
}

// An instance with no domain has no resolver, so Traefik never writes
// the file. That is a state to report, not a failure to explain — and
// the same goes for the empty file it creates before it has anything to
// put in it.
func TestNoStoreIsAState(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := readStore(StorePath(dir)); !errors.Is(err, ErrNoStore) {
		t.Errorf("a missing store answered %v", err)
	}

	path := StorePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readStore(path); !errors.Is(err, ErrNoStore) {
		t.Errorf("an empty store answered %v", err)
	}
}

// One unreadable entry must not hide the rest. The reason to open this
// page is the certificate that is about to expire, and it should still
// be on it.
func TestAnUnreadableEntryDoesNotHideTheOthers(t *testing.T) {
	dir := t.TempDir()
	path := writeStore(t, dir, "",
		entry("broken.example.com", "not base64 at all"),
		entry("fine.example.com", issue(t, "fine.example.com", time.Now().Add(time.Hour))))

	_, certs, err := readStore(path)
	if err != nil {
		t.Fatalf("readStore: %v", err)
	}
	if len(certs) != 1 || certs[0].Host != "fine.example.com" {
		t.Fatalf("read %+v", certs)
	}
}
