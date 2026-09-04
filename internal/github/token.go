package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// APIBase is GitHub's API root. A variable so a test can point it
// somewhere it controls: minting a token is the one thing here that
// cannot be checked against the real thing.
var APIBase = "https://api.github.com"

// jwtLifetime is how long the App's own assertion is good for. GitHub
// refuses anything over ten minutes, and clocks that disagree by a
// little are common, so this is deliberately short of the limit.
const jwtLifetime = 9 * time.Minute

// appJWT is the assertion that proves a request comes from this App. It
// is signed with the App's private key, which is the only credential
// GitHub issues an App — everything else is derived from it.
//
// Hand-rolled rather than pulled from a library: an RS256 JWT is a
// header, a payload and a signature over their base64, and a dependency
// for that is a dependency to keep current for no gain.
func appJWT(appID string, key *rsa.PrivateKey, now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	// iat is backdated by a minute: GitHub rejects an assertion issued
	// in its future, and a VPS clock a few seconds fast is enough.
	payload, err := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": appID,
	})
	if err != nil {
		return "", err
	}

	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign the app assertion: %w", err)
	}
	return signing + "." + enc.EncodeToString(signature), nil
}

// ParsePrivateKey reads the PEM GitHub hands out when an App is
// registered. It accepts both encodings GitHub has used.
func ParsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemText)))
	if block == nil {
		return nil, fmt.Errorf("the private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("the private key is neither PKCS#1 nor PKCS#8: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the private key is not RSA")
	}
	return key, nil
}

// token is one installation's access token and when it stops working.
type token struct {
	value   string
	expires time.Time
}

// tokenCache holds installation tokens until shortly before they expire.
//
// GitHub's are good for an hour, and a build that clones takes seconds
// of that. Minting one per clone would be a round trip against a rate
// limit for no reason; expiring early is what keeps a token from dying
// mid-clone.
type tokenCache struct {
	mu     sync.Mutex
	tokens map[int64]token
}

// tokenMargin is how long before expiry a cached token stops being
// offered. A clone that starts inside the margin still finishes.
const tokenMargin = 5 * time.Minute

func (c *tokenCache) get(installationID int64, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tokens[installationID]
	if !ok || now.Add(tokenMargin).After(t.expires) {
		return "", false
	}
	return t.value, true
}

func (c *tokenCache) put(installationID int64, value string, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokens == nil {
		c.tokens = map[int64]token{}
	}
	c.tokens[installationID] = token{value: value, expires: expires}
}

// mintToken exchanges the App's assertion for a token scoped to one
// installation. That token is what can read a private repository, and
// only the repositories that installation was granted.
func mintToken(ctx context.Context, client *http.Client, assertion string, installationID int64) (string, time.Time, error) {
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", APIBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("ask GitHub for an installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		// The body can name the reason — a suspended installation, a key
		// that no longer matches — and the status alone cannot.
		return "", time.Time{}, fmt.Errorf("GitHub refused an installation token: %s", resp.Status)
	}

	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("decode GitHub's token response: %w", err)
	}
	if body.Token == "" {
		return "", time.Time{}, fmt.Errorf("GitHub returned an empty installation token")
	}
	return body.Token, body.ExpiresAt, nil
}
