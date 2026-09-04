package store

import (
	"context"
	"testing"
)

func TestCreateAndListEnvironments(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	project, _, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")

	if _, err := s.CreateEnvironment(ctx, project.ID, "staging", "Staging"); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}

	envs, err := s.ListEnvironmentsForProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("ListEnvironmentsForProject: %v", err)
	}
	// production (created with the project) + staging
	if len(envs) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(envs))
	}
}

func TestSetAndGetEnvironmentEnv(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	_, env, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")

	if err := s.SetEnvironmentEnv(ctx, env.ID, map[string]string{"LOG_LEVEL": "debug"}); err != nil {
		t.Fatalf("SetEnvironmentEnv: %v", err)
	}

	got, err := s.GetEnvironmentByID(ctx, env.ID)
	if err != nil {
		t.Fatalf("GetEnvironmentByID: %v", err)
	}
	if got.Env["LOG_LEVEL"] != "debug" {
		t.Fatalf("unexpected env: %v", got.Env)
	}
}

func TestCountAppsInEnvironment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	project, env, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")

	n, err := s.CountAppsInEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatalf("CountAppsInEnvironment: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 apps, got %d", n)
	}

	s.CreateApp(ctx, org.ID, project.ID, env.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	n, err = s.CountAppsInEnvironment(ctx, env.ID)
	if err != nil {
		t.Fatalf("CountAppsInEnvironment: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 app, got %d", n)
	}
}

func TestDeleteEnvironment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	project, _, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")
	staging, _ := s.CreateEnvironment(ctx, project.ID, "staging", "Staging")

	if err := s.DeleteEnvironment(ctx, staging.ID); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := s.GetEnvironmentByID(ctx, staging.ID); err == nil {
		t.Fatal("expected the deleted environment to be gone")
	}
}
