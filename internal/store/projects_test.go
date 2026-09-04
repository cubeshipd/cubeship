package store

import (
	"context"
	"testing"
)

func TestCreateProjectWithDefaultEnvironment(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")

	project, env, err := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")
	if err != nil {
		t.Fatalf("CreateProjectWithDefaultEnvironment: %v", err)
	}
	if project.OrgID != org.ID {
		t.Fatalf("expected OrgID %d, got %d", org.ID, project.OrgID)
	}
	if env.ProjectID != project.ID {
		t.Fatalf("expected environment ProjectID %d, got %d", project.ID, env.ProjectID)
	}
	if env.Slug != ProductionEnvSlug {
		t.Fatalf("expected default environment slug %q, got %q", ProductionEnvSlug, env.Slug)
	}

	got, err := s.GetProjectBySlug(ctx, org.ID, "web")
	if err != nil {
		t.Fatalf("GetProjectBySlug: %v", err)
	}
	if got.Name != "Web" {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestListProjectsForOrg(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	orgA, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgB, _ := s.CreateOrganization(ctx, "globex", "Globex Corp")
	s.CreateProjectWithDefaultEnvironment(ctx, orgA.ID, "web", "Web")
	s.CreateProjectWithDefaultEnvironment(ctx, orgA.ID, "api", "API")
	s.CreateProjectWithDefaultEnvironment(ctx, orgB.ID, "web", "Web")

	projects, err := s.ListProjectsForOrg(ctx, orgA.ID)
	if err != nil {
		t.Fatalf("ListProjectsForOrg: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects for orgA, got %d", len(projects))
	}
}

func TestSetAndGetProjectEnv(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	project, _, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "web", "Web")

	if len(project.Env) != 0 {
		t.Fatalf("expected empty env on creation, got %v", project.Env)
	}

	if err := s.SetProjectEnv(ctx, project.ID, map[string]string{"DATABASE_URL": "postgres://shared"}); err != nil {
		t.Fatalf("SetProjectEnv: %v", err)
	}

	got, err := s.GetProjectByID(ctx, project.ID)
	if err != nil {
		t.Fatalf("GetProjectByID: %v", err)
	}
	if got.Env["DATABASE_URL"] != "postgres://shared" {
		t.Fatalf("unexpected env: %v", got.Env)
	}
}

// A project slug is only unique within its own organization: two
// different orgs can each have their own "web" project.
func TestProjectSlugUniqueWithinOrgOnly(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	orgA, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgB, _ := s.CreateOrganization(ctx, "globex", "Globex Corp")

	if _, _, err := s.CreateProjectWithDefaultEnvironment(ctx, orgA.ID, "web", "Web"); err != nil {
		t.Fatalf("create project in orgA: %v", err)
	}
	if _, _, err := s.CreateProjectWithDefaultEnvironment(ctx, orgB.ID, "web", "Web"); err != nil {
		t.Fatalf("create project with same slug in orgB: %v", err)
	}

	if _, _, err := s.CreateProjectWithDefaultEnvironment(ctx, orgA.ID, "web", "Web Again"); err == nil {
		t.Fatal("expected an error recreating the same slug within orgA")
	}
}
