package regauth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func parseCertPublicKey(t *testing.T, certPEM []byte) *rsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode cert PEM: no block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("certificate public key is not RSA: %T", cert.PublicKey)
	}
	return pub
}

func TestLoadOrCreateKeyPairPersistsAndReuses(t *testing.T) {
	dir := t.TempDir()

	key1, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	key2, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair (second call): %v", err)
	}
	if !key1.Equal(key2) {
		t.Fatal("expected the second call to reuse the persisted key")
	}
}

func TestIssueTokenVerifiesAgainstItsOwnCertificate(t *testing.T) {
	dir := t.TempDir()
	key, err := LoadOrCreateKeyPair(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	certPEM, err := SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}

	access := []AccessEntry{{Type: "repository", Name: "acme/myapp", Actions: []string{"pull", "push"}}}
	signed, err := IssueToken(key, "cubeship", "cubeship-registry", "lucas", access)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// Simulate what the registry does: parse the cert we handed it and
	// verify the token against the public key inside.
	pub := parseCertPublicKey(t, certPEM)

	parsed, err := jwt.ParseWithClaims(signed, &claims{}, func(t *jwt.Token) (any, error) {
		return pub, nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("token did not verify against its own certificate: %v", err)
	}

	got := parsed.Claims.(*claims)
	if got.Subject != "lucas" {
		t.Fatalf("expected subject lucas, got %q", got.Subject)
	}
	if len(got.Access) != 1 || got.Access[0].Name != "acme/myapp" {
		t.Fatalf("unexpected access claim: %+v", got.Access)
	}
}

func TestIssueTokenRejectsWrongKey(t *testing.T) {
	dir1, dir2 := t.TempDir(), t.TempDir()
	key1, _ := LoadOrCreateKeyPair(dir1)
	key2, _ := LoadOrCreateKeyPair(dir2)

	signed, err := IssueToken(key1, "cubeship", "cubeship-registry", "lucas", nil)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	_, err = jwt.ParseWithClaims(signed, &claims{}, func(t *jwt.Token) (any, error) {
		return &key2.PublicKey, nil
	})
	if err == nil {
		t.Fatal("expected verification against the wrong key to fail")
	}
}
