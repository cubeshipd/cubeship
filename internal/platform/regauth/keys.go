// Package regauth implements the signing side of Docker Registry v2
// token authentication: generating (and persisting) the daemon's own
// RSA keypair, a self-signed certificate the registry trusts to verify
// tokens, and issuing short-lived, scope-limited access tokens. See
// internal/api's token endpoint for the HTTP side of the protocol, and
// internal/bootstrap for how the registry container is pointed at both.
package regauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"
)

const keyBits = 2048

const keyFileName = "registry-signing-key.pem"

// LoadOrCreateKeyPair reads a PEM-encoded RSA private key from
// dataDir/registry-signing-key.pem, generating and persisting a new one
// (mode 0600) the first time it's called against a given data dir. This
// key signs every registry access token the daemon issues; it never
// leaves the process.
func LoadOrCreateKeyPair(dataDir string) (*rsa.PrivateKey, error) {
	path := dataDir + "/" + keyFileName

	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("decode PEM in %s: no block found", path)
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key in %s: %w", path, err)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("generate registry signing key: %w", err)
	}
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	return key, nil
}

// SelfSignedCert returns a PEM-encoded, long-lived self-signed
// certificate wrapping key's public half. The registry container trusts
// this certificate to verify tokens the daemon signs — it is never
// presented to anyone outside that trust relationship (it isn't served
// over TLS to end users), so a long expiry avoids needing rotation
// machinery for a personal-scale daemon. Regenerate it (delete the file
// bootstrap.WriteRegistryTokenCert writes) if the underlying key ever
// changes.
func SelfSignedCert(key *rsa.PrivateKey, commonName string) ([]byte, error) {
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create self-signed certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
