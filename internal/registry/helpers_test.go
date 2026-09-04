package registry_test

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/server/servertest"
)

// newSignedFixture is a fixture whose registry token endpoint can
// actually issue tokens: without a signing key it answers 503.
func newSignedFixture(t *testing.T) *servertest.Fixture {
	t.Helper()
	f := servertest.New(t)

	// 2048 bits is plenty and noticeably faster to generate than 4096,
	// which matters when every test in this file makes one.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	f.Server.SetRegistrySigningKey(key)
	return f
}

func httptestRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, target, nil)
}

func httptestPost(t *testing.T, target, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}
