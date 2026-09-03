package store

import (
	"context"
	"testing"
)

func TestCreateAndGetApp(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	created, err := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Domain != "myapp.example.com" || got.Image != "registry.example.com/myapp" {
		t.Fatalf("unexpected app: %+v", got)
	}
	if got.Status != "pending" {
		t.Fatalf("expected initial status 'pending', got %q", got.Status)
	}
}

func TestGetAppByImage(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	got, err := s.GetAppByImage(ctx, "registry.example.com/myapp")
	if err != nil {
		t.Fatalf("GetAppByImage: %v", err)
	}
	if got.Name != "myapp" {
		t.Fatalf("expected myapp, got %q", got.Name)
	}

	_, err = s.GetAppByImage(ctx, "registry.example.com/unknown")
	if err == nil {
		t.Fatal("expected error for unknown image")
	}
}

func TestUpdateAppContainer(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	created, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := s.UpdateAppContainer(ctx, created.ID, "abc123", "running"); err != nil {
		t.Fatalf("UpdateAppContainer: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "abc123" || got.Status != "running" {
		t.Fatalf("unexpected app after update: %+v", got)
	}
}

func TestSetAndGetAppEnv(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if len(app.Env) != 0 {
		t.Fatalf("expected empty env on creation, got %v", app.Env)
	}

	if err := s.SetAppEnv(ctx, app.ID, map[string]string{"PORT": "8080", "LOG_LEVEL": "info"}); err != nil {
		t.Fatalf("SetAppEnv: %v", err)
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Env["PORT"] != "8080" || got.Env["LOG_LEVEL"] != "info" {
		t.Fatalf("unexpected env: %v", got.Env)
	}
}
