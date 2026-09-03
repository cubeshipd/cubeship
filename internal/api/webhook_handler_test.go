package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type webhookFakeDocker struct {
	running      bool
	pulledRef    string
	createCalled bool
	logsContent  string
}

func (f *webhookFakeDocker) PullImage(ctx context.Context, ref string) error {
	f.pulledRef = ref
	return nil
}
func (f *webhookFakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.createCalled = true
	return "container-1", nil
}
func (f *webhookFakeDocker) StartContainer(ctx context.Context, id string) error  { return nil }
func (f *webhookFakeDocker) StopContainer(ctx context.Context, id string) error   { return nil }
func (f *webhookFakeDocker) RemoveContainer(ctx context.Context, id string) error { return nil }
func (f *webhookFakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return f.running, nil
}

func (f *webhookFakeDocker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logsContent)), nil
}

const registryNotificationPayload = `{
  "events": [
    {
      "action": "push",
      "target": {"repository": "myapp", "tag": "latest"}
    },
    {
      "action": "pull",
      "target": {"repository": "myapp", "tag": "latest"}
    },
    {
      "action": "push",
      "target": {"repository": "unknown-app", "tag": "latest"}
    }
  ]
}`

func TestRegistryWebhookTriggersDeployForMatchedApp(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 1
	orch.HealthCheckInterval = 0

	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(registryNotificationPayload)))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if docker.pulledRef != "registry.example.com/myapp:latest" {
		t.Fatalf("expected deploy to pull registry.example.com/myapp:latest, got %q", docker.pulledRef)
	}

	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" {
		t.Fatalf("expected app to be deployed, got container ID %q", app.ContainerID)
	}
}

func TestRegistryWebhookIgnoresUnknownRepository(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(`{
		"events": [{"action": "push", "target": {"repository": "unknown-app", "tag": "latest"}}]
	}`)))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even for unmatched repository, got %d", rec.Code)
	}
	if docker.createCalled {
		t.Fatal("expected no container to be created for an unknown repository")
	}
}

func TestRegistryWebhookRequiresNoAuth(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	orch := deploy.New(s, &webhookFakeDocker{})
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(`{"events":[]}`)))
	// deliberately no Authorization header
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d", rec.Code)
	}
}
