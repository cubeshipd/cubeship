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
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	// Containers run without a TTY, so the Engine multiplexes stdout and
	// stderr behind an 8-byte binary frame header. The handler must
	// demultiplex; copying the raw stream prints binary garbage.
	docker := &webhookFakeDocker{
		logsContent: dockerStdoutFrame("hello from the app\n") + dockerStdoutFrame("second line\n"),
	}
	srv := NewServer(s, deploy.New(s, docker), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil)
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
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}
