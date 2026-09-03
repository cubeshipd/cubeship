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

	docker := &webhookFakeDocker{logsContent: "hello from the app\n"}
	srv := NewServer(s, deploy.New(s, docker), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello from the app\n" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
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
