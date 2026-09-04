package app

import (
	"context"
	"testing"

	"cubeship/internal/extregistry"
	"cubeship/internal/org"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/project"
	"cubeship/internal/settings"
)

// externalFixture is an app that pulls from somebody else's registry,
// with the orchestrator wired to real credential storage.
func externalFixture(t *testing.T, image string) (*Orchestrator, *fakeDocker, *Scoped, *extregistry.Repository) {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)

	orgs := org.NewRepository(db)
	o, err := orgs.Create(ctx, "acme", "Acme")
	if err != nil {
		t.Fatal(err)
	}
	p, err := project.NewRepository(db).Create(ctx, o.ID, "web", "Web")
	if err != nil {
		t.Fatal(err)
	}
	env, err := project.NewEnvironmentRepository(db).Create(ctx, p.ID, "production", "Production")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(db).Create(ctx, o.ID, p.ID, env.ID,
		"myapp", "myapp.example.com", SourceExternal, Origin{Image: image}); err != nil {
		t.Fatal(err)
	}
	a, err := NewRepository(db).ScopedByReference(ctx, "acme", "web", "production", "myapp")
	if err != nil {
		t.Fatal(err)
	}

	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch := NewOrchestrator(db, docker, settings.NewService(db), extregistry.NewService(db, nil), nil, nil, testRegistry)
	orch.HealthCheckInterval = 0
	return orch, docker, a, extregistry.NewRepository(db)
}

// An external app deploys with no domain configured. That is the point
// of it: the embedded registry needs a domain before anything can be
// pushed, and this path needs nothing at all.
func TestExternalAppDeploysWithNoDomain(t *testing.T) {
	orch, docker, a, _ := externalFixture(t, "ghcr.io/acme/api")
	ctx := context.Background()

	if _, err := orch.Start(ctx, a.ID, "v2"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	pulled := docker.PulledRefs()
	if len(pulled) != 1 || pulled[0] != "ghcr.io/acme/api:v2" {
		t.Fatalf("pulled %v, want the external image at the tag asked for", pulled)
	}
}

// A public image needs no login, and the absence of one must not be an
// error: the registry is what refuses a pull it should not serve.
func TestAPublicImagePullsWithNoCredentials(t *testing.T) {
	orch, docker, a, _ := externalFixture(t, "nginx")
	ctx := context.Background()

	if _, err := orch.Start(ctx, a.ID, ""); err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	if docker.PullCreds() != nil {
		t.Errorf("a login was sent for a public image: %+v", docker.PullCreds())
	}
	if pulled := docker.PulledRefs(); len(pulled) != 1 || pulled[0] != "nginx:latest" {
		t.Fatalf("pulled %v, want nginx:latest", pulled)
	}
}

// The organization's credential for that registry is what the pull
// authenticates with. Matching is by host, so one login covers every
// image in it.
func TestAPrivateImagePullsWithTheOrgsCredential(t *testing.T) {
	orch, docker, a, creds := externalFixture(t, "ghcr.io/acme/api")
	ctx := context.Background()

	if _, err := creds.Create(ctx, a.OrgID, "GitHub", "ghcr.io", "acme-bot", "ghp_token"); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.Start(ctx, a.ID, ""); err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	got := docker.PullCreds()
	if got == nil {
		t.Fatal("the pull was anonymous despite a credential for that registry")
	}
	if got.Username != "acme-bot" || got.Password != "ghp_token" {
		t.Errorf("pulled as %+v", got)
	}
}

// A credential for a different registry is not a credential for this
// one. Sending it would leak a token to a host it was never issued for.
func TestACredentialForAnotherRegistryIsNotUsed(t *testing.T) {
	orch, docker, a, creds := externalFixture(t, "ghcr.io/acme/api")
	ctx := context.Background()

	if _, err := creds.Create(ctx, a.OrgID, "DO", "registry.digitalocean.com", "someone", "dop_token"); err != nil {
		t.Fatal(err)
	}
	if _, err := orch.Start(ctx, a.ID, ""); err != nil {
		t.Fatalf("Start: %v", err)
	}
	orch.Wait()

	if got := docker.PullCreds(); got != nil {
		t.Fatalf("a DigitalOcean token was sent to ghcr.io: %+v", got)
	}
}
