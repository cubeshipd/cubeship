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
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	created, err := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/acme/myapp")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if created.OrgID != org.ID {
		t.Fatalf("expected OrgID %d, got %d", org.ID, created.OrgID)
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Domain != "myapp.example.com" || got.Image != "registry.example.com/acme/myapp" {
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
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

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
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	created, _ := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

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
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	app, _ := s.CreateApp(ctx, org.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

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

func TestListAppsIncludesOrgID(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	orgA, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgB, _ := s.CreateOrganization(ctx, "globex", "Globex Corp")
	appA, _ := s.CreateApp(ctx, orgA.ID, "appa", "appa.example.com", "registry.example.com/appa")
	appB, _ := s.CreateApp(ctx, orgB.ID, "appb", "appb.example.com", "registry.example.com/appb")

	apps, err := s.ListApps(ctx)
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps, got %d", len(apps))
	}

	byName := map[string]*App{}
	for _, a := range apps {
		byName[a.Name] = a
	}
	if byName["appa"].OrgID != appA.OrgID {
		t.Fatalf("expected appa OrgID %d, got %d", appA.OrgID, byName["appa"].OrgID)
	}
	if byName["appb"].OrgID != appB.OrgID {
		t.Fatalf("expected appb OrgID %d, got %d", appB.OrgID, byName["appb"].OrgID)
	}
	if byName["appa"].OrgID == byName["appb"].OrgID {
		t.Fatal("expected apps to belong to different orgs")
	}
}
