package regauth

import (
	"crypto/rsa"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenTTL is how long an issued registry access token remains valid.
// Docker's CLI/daemon re-requests a token transparently on expiry, so a
// short TTL (matching what registries like Docker Hub use) limits how
// long a leaked token stays useful without adding real friction.
const TokenTTL = 5 * time.Minute

// AccessEntry is one granted scope in an issued token, matching the
// Docker Registry v2 token specification's "access" claim shape.
type AccessEntry struct {
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

type claims struct {
	jwt.RegisteredClaims
	Access []AccessEntry `json:"access"`
}

// IssueToken signs a registry access token granting exactly access,
// scoped to subject (the authenticated username, or "cubeshipd" for the
// daemon's own pulls). audience is the registry's configured "service"
// name (see bootstrap.RegistryConfigYAML's auth.token.service).
func IssueToken(key *rsa.PrivateKey, issuer, audience, subject string, access []AccessEntry) (string, error) {
	now := time.Now()
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Access: access,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
	signed, err := token.SignedString(key)
	if err != nil {
		return "", err
	}
	return signed, nil
}
