package extregistry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ghcrFake is a GHCR whose token server refuses any login and hands an
// anonymous token to anyone, and which lists tags for that token only
// when the package is public.
func ghcrFake(public bool) *http.Client {
	reply := func(status int, header http.Header, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	return &http.Client{Transport: packageTransport(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "ghcr.io" && req.URL.Path == "/token":
			if _, _, login := req.BasicAuth(); login || !public {
				return reply(http.StatusForbidden, nil, `{"errors":[{"code":"DENIED"}]}`)
			}
			return reply(http.StatusOK, nil, `{"token":"anon"}`)
		case req.URL.Host == "ghcr.io" && req.URL.Path == "/v2/acme/api/tags/list":
			if req.Header.Get("Authorization") != "Bearer anon" {
				challenge := http.Header{"Www-Authenticate": {`Bearer realm="https://ghcr.io/token",service="ghcr.io"`}}
				return reply(http.StatusUnauthorized, challenge, `{}`)
			}
			return reply(http.StatusOK, nil, `{"tags":["1.12.3"]}`)
		}
		return reply(http.StatusNotFound, nil, `{}`)
	})}
}

func TestARefusedLoginStillListsAPublicImage(t *testing.T) {
	client := ghcrFake(true)
	s := &Service{client: client, publicClient: client}
	login := &Credential{Host: "ghcr.io", Provider: ProviderGeneric, Username: "me", Password: "no-package-scope"}

	images, err := s.tagsWith(context.Background(), login, "ghcr.io/acme/api", "acme/api")
	if err != nil || len(images) != 1 || images[0].Tag != "1.12.3" {
		t.Fatalf("images = %+v, err = %v", images, err)
	}
}

func TestAPrivateImageReportsTheRefusedLogin(t *testing.T) {
	client := ghcrFake(false)
	s := &Service{client: client, publicClient: client}
	login := &Credential{Host: "ghcr.io", Provider: ProviderGeneric, Username: "me", Password: "no-package-scope"}

	if _, err := s.tagsWith(context.Background(), login, "ghcr.io/acme/api", "acme/api"); !errors.Is(err, errUnauthorized) {
		t.Fatalf("err = %v, want the login's refusal", err)
	}
}
