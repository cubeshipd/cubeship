package regauth

import (
	"crypto/rsa"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenIssuer and TokenService must match the registry's config.yml
// (see bootstrap.RegistryConfigYAML's auth.token.issuer/service) —
// they're claims the registry itself validates on every token it
// receives before trusting the signature.
const (
	TokenIssuer  = "cubeship"
	TokenService = "cubeship-registry"
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

// claims is the token as the registry reads it, which is not quite what
// jwt.RegisteredClaims writes.
//
// `aud` is a plain string here, and that is the whole reason this type
// spells the registered claims out instead of embedding them.
// RegisteredClaims types the audience as jwt.ClaimStrings, and
// golang-jwt v5 marshals even a single audience as a JSON *array*. The
// distribution registry's own ClaimSet types `aud` as a string, so it
// refuses the token before looking at the signature:
//
//	error while unmarshalling raw token: json: cannot unmarshal array
//	into Go struct field ClaimSet.aud of type string
//
// which reaches a caller as a bare 401 with nothing about audiences in
// it. The spec allows both shapes; this registry accepts one.
type claims struct {
	Issuer    string        `json:"iss"`
	Subject   string        `json:"sub"`
	Audience  string        `json:"aud"`
	ExpiresAt int64         `json:"exp"`
	NotBefore int64         `json:"nbf"`
	IssuedAt  int64         `json:"iat"`
	JTI       string        `json:"jti"`
	Access    []AccessEntry `json:"access"`
}

// GetExpirationTime and the four that follow are jwt.Claims. They exist
// so this type can be signed and parsed by the library; nothing here
// reads them.
func (c claims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}
func (c claims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}
func (c claims) GetNotBefore() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.NotBefore, 0)), nil
}
func (c claims) GetIssuer() (string, error)             { return c.Issuer, nil }
func (c claims) GetSubject() (string, error)            { return c.Subject, nil }
func (c claims) GetAudience() (jwt.ClaimStrings, error) { return jwt.ClaimStrings{c.Audience}, nil }

// IssueToken signs a registry access token granting exactly access,
// scoped to subject (the authenticated username, or "cubeshipd" for the
// daemon's own pulls). audience is the registry's configured "service"
// name (see bootstrap.RegistryConfigYAML's auth.token.service).
func IssueToken(key *rsa.PrivateKey, certDER []byte, issuer, audience, subject string, access []AccessEntry) (string, error) {
	now := time.Now()
	c := claims{
		Issuer:    issuer,
		Subject:   subject,
		Audience:  audience,
		ExpiresAt: now.Add(TokenTTL).Unix(),
		NotBefore: now.Add(-time.Minute).Unix(),
		IssuedAt:  now.Unix(),
		Access:    access,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, c)

	// x5c carries the certificate that vouches for the signing key.
	//
	// Without it the registry has no way to *find* a key to verify
	// with: it looks for x5c or jwk in the header and fails with a bare
	// "invalid token" when there is neither. The certificate is the same
	// one it holds as its trust root, so what it verifies is that this
	// token was signed by the key it was already told to trust.
	if len(certDER) > 0 {
		token.Header["x5c"] = []string{base64.StdEncoding.EncodeToString(certDER)}
	}

	signed, err := token.SignedString(key)
	if err != nil {
		return "", err
	}
	return signed, nil
}
