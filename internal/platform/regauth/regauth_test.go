package regauth

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
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
	certPEM, certDER, err := SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}

	access := []AccessEntry{{Type: "repository", Name: "acme/myapp", Actions: []string{"pull", "push"}}}
	signed, err := IssueToken(key, certDER, "cubeship", "cubeship-registry", "lucas", access)
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

	signed, err := IssueToken(key1, nil, "cubeship", "cubeship-registry", "lucas", nil)
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

// The two things the distribution registry needs that verifying a
// signature by hand never checks — and which is why the tests above
// passed while every token this daemon issued was refused.
//
// A token is JSON, and the registry reads it with its own struct. Both
// of these are about that struct, not about cryptography.
func TestIssuedTokensAreShapedTheWayTheRegistryReadsThem(t *testing.T) {
	key, err := LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	_, certDER, err := SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}

	signed, err := IssueToken(key, certDER, TokenIssuer, TokenService, "lucas", nil)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	parts := strings.Split(signed, ".")
	if len(parts) != 3 {
		t.Fatalf("expected a three-part JWT, got %d parts", len(parts))
	}
	decode := func(part string) map[string]any {
		raw, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}

	// aud is a string. The registry's ClaimSet types it as one, and
	// golang-jwt's RegisteredClaims marshals even a single audience as
	// an array — which the registry rejects before looking at the
	// signature, with a 401 that says nothing about audiences.
	if aud, ok := decode(parts[1])["aud"].(string); !ok || aud != TokenService {
		t.Errorf("aud is %#v, want the service name as a plain string", decode(parts[1])["aud"])
	}

	// x5c carries the certificate. Without it the registry has no way to
	// find a key to verify with and answers a bare "invalid token".
	chain, ok := decode(parts[0])["x5c"].([]any)
	if !ok || len(chain) == 0 {
		t.Fatalf("no x5c in the header: %#v", decode(parts[0]))
	}
	if chain[0] != base64.StdEncoding.EncodeToString(certDER) {
		t.Error("x5c does not carry the certificate the registry was given as its trust root")
	}
}

// The same key must always produce the same certificate.
//
// The registry reads its trust root once, when its container starts, and
// it accepts only that exact certificate — one regenerated over the same
// key does not chain to it. A certificate that varied meant the registry
// answered 401 to a daemon holding the very key it trusts, from the
// first daemon restart until someone restarted the registry too. That
// looked like a broken install and was a clock.
func TestTheCertificateIsTheSameEveryTimeForOneKey(t *testing.T) {
	key, err := LoadOrCreateKeyPair(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}

	firstPEM, firstDER, err := SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}
	secondPEM, secondDER, err := SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}

	if !bytes.Equal(firstDER, secondDER) {
		t.Error("two certificates over one key differ, so a daemon restart invalidates every token the registry will accept")
	}
	if !bytes.Equal(firstPEM, secondPEM) {
		t.Error("the PEM differs between calls, so the trust root on disk churns on every start")
	}
}
