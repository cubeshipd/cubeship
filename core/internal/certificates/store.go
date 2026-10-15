package certificates

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"

	"cubeship/internal/app"
)

// StorePath is where Traefik keeps what it was issued, inside the data
// directory. It is the host side of the bind mount Traefik is given as
// /letsencrypt, and the daemon has the data directory mounted at the
// same path — which is what lets this be read at all.
//
// It must match bootstrap.TraefikContainerOpts.
func StorePath(dataDir string) string {
	return filepath.Join(dataDir, "letsencrypt", "acme.json")
}

// acmeStore is acme.json: one entry per resolver, each holding the ACME
// account and everything issued through it. Cubeship configures one
// resolver, "letsencrypt", but the file's shape is a map and reading it
// as one costs nothing.
type acmeStore map[string]struct {
	Account struct {
		Email string `json:"Email"`
	} `json:"Account"`
	Certificates []storedCertificate `json:"Certificates"`
}

// storedCertificate is one entry. The certificate and key are
// base64-encoded PEM; the key is never decoded here — nothing above
// this could do anything with it that is not a leak.
type storedCertificate struct {
	Domain struct {
		Main string   `json:"main"`
		SANs []string `json:"sans"`
	} `json:"domain"`
	Certificate string `json:"certificate"`
}

// readStore parses the ACME store and returns what it holds, newest
// expiry last.
//
// A store that is not there is ErrNoStore rather than an error: an
// instance with no domain has no resolver, so Traefik never writes the
// file, and that is a state to report rather than a failure to explain.
func readStore(path string) (email string, certs []Certificate, err error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil, ErrNoStore
	}
	if err != nil {
		return "", nil, fmt.Errorf("read the ACME store at %s: %w", path, err)
	}
	// Traefik creates the file before it has anything to put in it.
	if len(raw) == 0 {
		return "", nil, ErrNoStore
	}

	var store acmeStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return "", nil, fmt.Errorf("parse the ACME store at %s: %w", path, err)
	}

	for _, resolver := range store {
		if email == "" {
			email = resolver.Account.Email
		}
		for _, stored := range resolver.Certificates {
			c, err := parseStored(stored)
			if err != nil {
				// One unreadable entry must not hide the rest: the
				// point of this page is the ones that are about to
				// expire.
				continue
			}
			certs = append(certs, c)
		}
	}
	sort.Slice(certs, func(i, j int) bool {
		if certs[i].NotAfter.Equal(certs[j].NotAfter) {
			return certs[i].Host < certs[j].Host
		}
		return certs[i].NotAfter.Before(certs[j].NotAfter)
	})
	return email, certs, nil
}

// parseStored turns one entry into what a reader wants: the leaf
// certificate's own facts. The chain below it is the CA's, and says
// nothing about this name.
func parseStored(stored storedCertificate) (Certificate, error) {
	der, err := base64.StdEncoding.DecodeString(stored.Certificate)
	if err != nil {
		return Certificate{}, fmt.Errorf("decode the certificate for %q: %w", stored.Domain.Main, err)
	}
	block, _ := pem.Decode(der)
	if block == nil {
		return Certificate{}, fmt.Errorf("no PEM block in the certificate for %q", stored.Domain.Main)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Certificate{}, fmt.Errorf("parse the certificate for %q: %w", stored.Domain.Main, err)
	}

	host := stored.Domain.Main
	if host == "" && len(leaf.DNSNames) > 0 {
		host = leaf.DNSNames[0]
	}
	return Certificate{
		Host:      app.NormalizeHost(host),
		SANs:      otherNames(host, leaf.DNSNames),
		Issuer:    leaf.Issuer.CommonName,
		NotBefore: leaf.NotBefore.UTC(),
		NotAfter:  leaf.NotAfter.UTC(),
		Serial:    serialOf(leaf.SerialNumber),
	}, nil
}

// otherNames is every name on the certificate except the one it is
// filed under, which is already the row's title.
func otherNames(main string, names []string) []string {
	var out []string
	for _, name := range names {
		if !equalHost(name, main) {
			out = append(out, app.NormalizeHost(name))
		}
	}
	return out
}

// serialOf renders a serial the way a CA writes it: hex, colon
// separated, which is what a support thread about one asks for.
func serialOf(serial *big.Int) string {
	if serial == nil {
		return ""
	}
	raw := serial.Bytes()
	if len(raw) == 0 {
		return "00"
	}
	out := make([]byte, 0, len(raw)*3-1)
	const hex = "0123456789abcdef"
	for i, b := range raw {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, hex[b>>4], hex[b&0x0f])
	}
	return string(out)
}

// equalHost compares two names the way a browser and Traefik do.
func equalHost(a, b string) bool {
	return app.NormalizeHost(a) == app.NormalizeHost(b)
}
