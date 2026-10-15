package extregistry

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type packageTransport func(*http.Request) (*http.Response, error)

func (fn packageTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestGHCRPublicationDates(t *testing.T) {
	client := &http.Client{Transport: packageTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" || req.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("unexpected Packages request: %s", req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[{"created_at":"2026-09-29T14:43:43Z","metadata":{"container":{"tags":["0.3.0","latest"]}}}]`)),
		}, nil
	})}
	images := []Image{{Tag: "0.1.0"}, {Tag: "0.3.0"}, {Tag: "latest"}}
	ghcrPublishedAt(context.Background(), client, "token", "lucasaarch/jian-gateway", images)
	if images[0].PushedAt != nil || images[1].PushedAt == nil || images[2].PushedAt == nil ||
		images[1].PushedAt.Format("2006-01-02T15:04:05Z") != "2026-09-29T14:43:43Z" {
		t.Fatalf("wrong publication dates: %+v", images)
	}
}
