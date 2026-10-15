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
	"io"
	"net/http"
	"net/url"
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

// RepositoryRef is one repository an installation can reach, and a
// branch of it. Both are what a person picks from rather than types.
type RepositoryRef struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	Default  string `json:"default_branch"`
}

// Branch is one branch of a repository.
type Branch struct {
	Name string `json:"name"`
}

// listInstallationRepositories asks GitHub what an installation was
// granted. It is the whole of it: someone who installed the App on three
// repositories should see three, not every repository they own.
//
// Paginated, because an organization with a hundred repositories is
// ordinary and one page is thirty.
func listInstallationRepositories(ctx context.Context, client *http.Client, token string) ([]RepositoryRef, error) {
	var out []RepositoryRef

	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", APIBase, page)
		var body struct {
			TotalCount   int             `json:"total_count"`
			Repositories []RepositoryRef `json:"repositories"`
		}
		if err := getJSON(ctx, client, url, token, &body); err != nil {
			return nil, err
		}
		out = append(out, body.Repositories...)
		if len(body.Repositories) < 100 {
			break
		}
	}
	return out, nil
}

// listBranches asks GitHub for a repository's branches.
func listBranches(ctx context.Context, client *http.Client, token, fullName string) ([]Branch, error) {
	var out []Branch

	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("%s/repos/%s/branches?per_page=100&page=%d", APIBase, fullName, page)
		var body []Branch
		if err := getJSON(ctx, client, url, token, &body); err != nil {
			return nil, err
		}
		out = append(out, body...)
		if len(body) < 100 {
			break
		}
	}
	return out, nil
}

// maxPages bounds a listing. A repository with more branches than this
// is one nobody is picking from a dropdown anyway, and an unbounded loop
// against someone else's API is not something to leave in a deploy path.
const maxPages = 10

// getJSON is a read from GitHub with an installation token.
func getJSON(ctx context.Context, client *http.Client, url, token string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ask GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 404 here is usually not "no such thing" but "this
		// installation was not granted it", and saying so is the
		// difference between a typo and a permission to widen.
		if resp.StatusCode == http.StatusNotFound {
			return ErrNotGranted
		}
		return fmt.Errorf("GitHub answered %s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return fmt.Errorf("decode GitHub's answer: %w", err)
	}
	return nil
}

// ManifestApp is what GitHub hands back when a manifest is converted:
// an App, already created, with its credentials.
//
// This is the whole point of the manifest flow. The alternative is
// asking someone to create an App by hand and paste four values, one of
// which is a private key.
type ManifestApp struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	PEM           string `json:"pem"`
	WebhookSecret string `json:"webhook_secret"`
	// The OAuth half of the App, which is what proves who is connecting
	// an installation rather than merely naming one. See
	// exchangeUserCode.
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// convertManifest exchanges the code GitHub redirects back with for the
// App it just created.
//
// The code is the credential — this call carries no other — and it is
// single-use and short-lived, which is why it goes straight from the
// browser to here and is spent immediately.
func convertManifest(ctx context.Context, client *http.Client, code string) (*ManifestApp, error) {
	url := fmt.Sprintf("%s/app-manifests/%s/conversions", APIBase, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange the manifest code with GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		// The commonest cause is a code already spent or expired, and
		// the answer to both is to start the flow again.
		return nil, fmt.Errorf("GitHub refused the manifest code (%s); it is single-use and expires in an hour", resp.Status)
	}

	var app ManifestApp
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, fmt.Errorf("decode GitHub's answer: %w", err)
	}
	if app.ID == 0 || app.PEM == "" {
		return nil, fmt.Errorf("GitHub returned an app with no credentials")
	}
	return &app, nil
}

// exchangeUserCode turns the code GitHub redirects back with, after
// someone installs the App, into a token that acts as *that person*.
//
// This is the whole reason the App asks for OAuth on install. Without
// it, connecting an installation is a caller naming a number: the
// daemon had no way to tell an operator connecting their own
// installation from one naming somebody else's, and minting tokens for
// a stranger's installation is read access to their private code.
//
// The token is used once, immediately, to ask GitHub which
// installations this person can reach. Nothing keeps it: it is a
// credential for someone else's account, and the answer is what matters.
func exchangeUserCode(ctx context.Context, client *http.Client, clientID, clientSecret, code string) (string, error) {
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange the install code with GitHub: %w", err)
	}
	defer resp.Body.Close()

	var answer struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return "", fmt.Errorf("decode GitHub's answer: %w", err)
	}
	// GitHub answers 200 with an error field for a spent or wrong code,
	// so the status alone says nothing.
	if answer.Error != "" {
		reason := answer.ErrorDescription
		if reason == "" {
			reason = answer.Error
		}
		return "", fmt.Errorf("GitHub refused the install code: %s", reason)
	}
	if answer.AccessToken == "" {
		return "", fmt.Errorf("GitHub returned no token for the install code")
	}
	return answer.AccessToken, nil
}

// userInstallation is one installation as the person who can reach it
// sees it.
type userInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
	} `json:"account"`
}

// listUserInstallations asks GitHub which of this App's installations a
// person can reach.
//
// It is the verification: GitHub answers with exactly the installations
// that user administers, so an id that is not in the list is one they do
// not own — whatever they typed. The account name comes from here too,
// rather than from the caller, so what is stored is GitHub's answer.
func listUserInstallations(ctx context.Context, client *http.Client, userToken string) ([]userInstallation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/user/installations?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask GitHub which installations you can reach: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s when asked for your installations", resp.Status)
	}
	var answer struct {
		Installations []userInstallation `json:"installations"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&answer); err != nil {
		return nil, fmt.Errorf("decode GitHub's answer: %w", err)
	}
	return answer.Installations, nil
}
