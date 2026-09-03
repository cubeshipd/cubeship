package store

import (
	"context"
	"testing"
)

func TestRecordAndListDeployments(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := s.RecordDeployment(ctx, app.ID, "registry.example.com/myapp:latest", "success", ""); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
	if err := s.RecordDeployment(ctx, app.ID, "registry.example.com/myapp:v2", "failed", "health check timeout"); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	deps, err := s.ListDeployments(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(deps))
	}
	if deps[1].Status != "failed" || deps[1].Error != "health check timeout" {
		t.Fatalf("unexpected second deployment: %+v", deps[1])
	}
}
