package extregistry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGHCRPublicTagsUseImageCreationDate(t *testing.T) {
	client := &http.Client{Transport: packageTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/v2/acme/api/manifests/0.4.0":
			body = `{"manifests":[{"digest":"sha256:child","platform":{"os":"linux","architecture":"amd64"}}]}`
		case "/v2/acme/api/manifests/sha256:child":
			body = `{"config":{"digest":"sha256:config"}}`
		case "/v2/acme/api/blobs/sha256:config":
			body = `{"created":"2026-09-29T14:43:43Z"}`
		default:
			t.Fatalf("unexpected GHCR request: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	images := []Image{{Tag: "0.4.0", Digest: "sha256:image"}, {Tag: "0.4", Digest: "sha256:image"}}
	ghcrCreatedAt(context.Background(), client, &Credential{Host: "ghcr.io"}, "acme/api", images)
	if images[0].CreatedAt == nil || images[1].CreatedAt == nil ||
		images[0].CreatedAt.Format("2006-01-02T15:04:05Z") != "2026-09-29T14:43:43Z" {
		t.Fatalf("missing build dates: %+v", images)
	}
}
