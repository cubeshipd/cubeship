package github_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cubeship/internal/github"
	"cubeship/internal/server/servertest"
)

// generateKey makes an App private key in the PEM GitHub hands out.
func generateKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, string(encoded)
}

// A key GitHub issued has to be readable. Both encodings it has used
// over the years turn up in the wild.
func TestParsePrivateKeyTakesBothEncodings(t *testing.T) {
	key, pkcs1 := generateKey(t)
	if _, err := github.ParsePrivateKey(pkcs1); err != nil {
		t.Errorf("PKCS#1: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if _, err := github.ParsePrivateKey(pkcs8); err != nil {
		t.Errorf("PKCS#8: %v", err)
	}

	for _, bad := range []string{"", "not a key", "-----BEGIN RSA PRIVATE KEY-----\nzzz\n-----END RSA PRIVATE KEY-----"} {
		if _, err := github.ParsePrivateKey(bad); err == nil {
			t.Errorf("%q was accepted as a private key", bad)
		}
	}
}

// claims is the middle segment of the assertion GitHub is sent.
func claims(t *testing.T, assertion string) map[string]any {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("the assertion is not a JWT: %q", assertion)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode the assertion's payload: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// githubStub stands in for api.github.com. Minting a token is the one
// thing here that cannot be checked against the real thing, so this
// checks the request Cubeship makes and answers as GitHub would.
type githubStub struct {
	*httptest.Server
	calls  atomic.Int64
	expiry time.Duration
}

func newGitHubStub(t *testing.T, expiry time.Duration) *githubStub {
	t.Helper()
	stub := &githubStub{expiry: expiry}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls.Add(1)

		if r.URL.Path != "/app/installations/42/access_tokens" {
			t.Errorf("Cubeship asked for %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("the token was requested with %s", r.Method)
		}

		assertion, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			t.Errorf("the request carries no bearer assertion: %q", r.Header.Get("Authorization"))
		}
		got := claims(t, assertion)
		if got["iss"] != "12345" {
			t.Errorf("the assertion is issued by %v, want the App id", got["iss"])
		}
		// GitHub refuses an assertion good for more than ten minutes,
		// or issued in its own future.
		iat, _ := got["iat"].(float64)
		exp, _ := got["exp"].(float64)
		if exp-iat > 600 {
			t.Errorf("the assertion lives %.0fs, which GitHub refuses", exp-iat)
		}
		if iat > float64(time.Now().Unix()) {
			t.Errorf("the assertion is issued in the future")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_installation_token",
			"expires_at": time.Now().Add(stub.expiry),
		})
	}))
	t.Cleanup(stub.Close)
	return stub
}

// pointAtStub aims the module at a GitHub that is not GitHub, and puts
// it back afterwards.
func pointAtStub(t *testing.T, url string) {
	t.Helper()
	previous := github.APIBase
	github.APIBase = url
	t.Cleanup(func() { github.APIBase = previous })
}

// registerApp configures the instance as an App with a real key.
func registerApp(t *testing.T, f *servertest.Fixture) {
	t.Helper()
	_, privateKey := generateKey(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/settings", map[string]string{
		"github_app_id":         "12345",
		"github_private_key":    privateKey,
		"github_webhook_secret": webhookSecret,
	}, f.AdminKey), http.StatusOK)
}

// The token a clone of a private repository authenticates with, all the
// way from the App's key to what GitHub hands back.
func TestTokenForRepository(t *testing.T) {
	f := servertest.New(t)
	registerApp(t, f)
	connect(t, f, 42, "acme")

	stub := newGitHubStub(t, time.Hour)
	pointAtStub(t, stub.URL)

	token, found, err := f.Server.GitHub.TokenForRepository(
		context.Background(), f.Org.ID, "https://github.com/acme/api.git")
	if err != nil {
		t.Fatalf("TokenForRepository: %v", err)
	}
	if !found || token != "ghs_installation_token" {
		t.Fatalf("got %q, found=%v", token, found)
	}

	// GitHub's tokens last an hour and a clone takes seconds of that.
	// Minting one per clone would be a round trip against a rate limit
	// for nothing.
	if _, _, err := f.Server.GitHub.TokenForRepository(
		context.Background(), f.Org.ID, "https://github.com/acme/other"); err != nil {
		t.Fatal(err)
	}
	if calls := stub.calls.Load(); calls != 1 {
		t.Errorf("GitHub was asked %d times for a token it had already given", calls)
	}
}

// A token about to expire is no use to a clone that is about to start.
func TestATokenNearExpiryIsReplaced(t *testing.T) {
	f := servertest.New(t)
	registerApp(t, f)
	connect(t, f, 42, "acme")

	stub := newGitHubStub(t, time.Minute) // inside the safety margin
	pointAtStub(t, stub.URL)

	for range 2 {
		if _, _, err := f.Server.GitHub.TokenForRepository(
			context.Background(), f.Org.ID, "https://github.com/acme/api"); err != nil {
			t.Fatal(err)
		}
	}
	if calls := stub.calls.Load(); calls != 2 {
		t.Errorf("a token expiring in a minute was reused (%d calls)", calls)
	}
}

// Nothing found is not an error: a public repository needs no token, and
// letting GitHub refuse a private one beats refusing a clone that would
// have worked.
func TestNoInstallationMeansNoToken(t *testing.T) {
	f := servertest.New(t)
	registerApp(t, f)

	for _, url := range []string{
		"https://github.com/acme/api.git", // on GitHub, but not connected
		"https://gitlab.com/acme/api.git", // not on GitHub at all
	} {
		token, found, err := f.Server.GitHub.TokenForRepository(context.Background(), f.Org.ID, url)
		if err != nil {
			t.Errorf("%s: %v", url, err)
		}
		if found || token != "" {
			t.Errorf("%s produced a token: %q", url, token)
		}
	}
}

// GitHub refusing has to reach the deploy that asked, not be swallowed
// into an anonymous clone that fails later with something unrelated.
func TestGitHubRefusingIsAnError(t *testing.T) {
	f := servertest.New(t)
	registerApp(t, f)
	connect(t, f, 42, "acme")

	refuses := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Bad credentials"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(refuses.Close)
	pointAtStub(t, refuses.URL)

	_, _, err := f.Server.GitHub.TokenForRepository(
		context.Background(), f.Org.ID, "https://github.com/acme/api")
	if err == nil {
		t.Fatal("GitHub refusing was reported as no token rather than as a failure")
	}
	if !strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("the error does not carry what GitHub said: %v", err)
	}
}
