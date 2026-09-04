package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestGetLogsStreamsContainerOutput(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	app, _ := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")
	key := testAPIKeyFor(t, s, true)

	// Containers run without a TTY, so the Engine multiplexes stdout and
	// stderr behind an 8-byte binary frame header. The handler must
	// demultiplex; copying the raw stream prints binary garbage.
	docker := &webhookFakeDocker{
		logsContent: dockerStdoutFrame("hello from the app\n") + dockerStdoutFrame("second line\n"),
	}
	srv := NewServer(s, deploy.New(s, docker), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello from the app\nsecond line\n" {
		t.Fatalf("expected the demultiplexed log lines, got %q", rec.Body.String())
	}
}

func TestGetLogsBeforeFirstDeploy(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}
