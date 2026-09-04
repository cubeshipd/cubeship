package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestManualDeployEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "webhook-secret", "registry.example.com")

	body, _ := json.Marshal(map[string]string{"tag": "v2"})
	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if docker.pulledRef != "127.0.0.1:5000/acme/myapp:v2" {
		t.Fatalf("expected pull of 127.0.0.1:5000/acme/myapp:v2, got %q", docker.pulledRef)
	}
}

func TestManualDeployDefaultsToLatestTag(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`), key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if docker.pulledRef != "127.0.0.1:5000/acme/myapp:latest" {
		t.Fatalf("expected default tag latest pulled over loopback, got %q", docker.pulledRef)
	}
}

func TestSetEnvEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	key := testAPIKeyFor(t, s, true)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"PORT": "9090"}})
	req := authedRequest(http.MethodPut, "/apps/myapp/env", body, key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Env["PORT"] != "9090" {
		t.Fatalf("expected env to be persisted, got %v", got.Env)
	}
}

func TestManualDeployUnknownApp(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	key := testAPIKeyFor(t, s, true)
	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/nope/deploy", []byte(`{}`), key)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestManualDeployHidesAppFromOtherOrgs(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	outsiderKey := testAPIKeyFor(t, s, false)

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "webhook-secret", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`), outsiderKey)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (don't reveal the app exists to an outsider), got %d", rec.Code)
	}
}
