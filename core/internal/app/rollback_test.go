package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cubeship/internal/platform/database/dbtest"
)

func TestRollbackTarget(t *testing.T) {
	const sha = "a8ef4fa0123456789abcdef0123456789abcdef0"
	scoped := func(source Source, image string) *Scoped {
		return &Scoped{
			App:         App{Name: "api", Source: string(source), SourceImage: image},
			ProjectSlug: "web", EnvironmentSlug: "production",
		}
	}
	ok := func(image, gitSHA string) *Deployment {
		return &Deployment{Status: DeploymentSucceeded, ImageRef: image, GitSHA: gitSHA, GitBranch: "main"}
	}

	cases := []struct {
		name     string
		app      *Scoped
		d        *Deployment
		tag      string
		identity DeploymentIdentity
	}{
		{"registry tag", scoped(SourceRegistry, ""), ok(testRegistry+"/web/production/api:v1", ""), "v1", DeploymentIdentity{}},
		{"external tag", scoped(SourceExternal, "ghcr.io/acme/api"), ok("ghcr.io/acme/api:1.4", ""), "1.4", DeploymentIdentity{}},
		{"built commit", scoped(SourceDockerfile, ""), ok("cubeship-build/web/production/api:main", sha), sha, DeploymentIdentity{GitSHA: sha, GitBranch: "main"}},
		{"railpack commit", scoped(SourceRailpack, ""), ok("cubeship-build/web/production/api:main", sha), sha, DeploymentIdentity{GitSHA: sha, GitBranch: "main"}},
	}
	for _, c := range cases {
		tag, identity, err := rollbackTarget(c.app, c.d)
		if err != nil {
			t.Errorf("%s: refused: %v", c.name, err)
			continue
		}
		if tag != c.tag || identity != c.identity {
			t.Errorf("%s: got tag %q identity %+v, want %q %+v", c.name, tag, identity, c.tag, c.identity)
		}
	}

	refused := []struct {
		name string
		app  *Scoped
		d    *Deployment
	}{
		{"failed", scoped(SourceRegistry, ""), &Deployment{Status: DeploymentFailed, ImageRef: testRegistry + "/web/production/api:v1"}},
		{"pending", scoped(SourceRegistry, ""), &Deployment{Status: DeploymentPending, ImageRef: testRegistry + "/web/production/api:v1"}},
		{"live", scoped(SourceRegistry, ""), &Deployment{Status: DeploymentSucceeded, ImageRef: testRegistry + "/web/production/api:v1", Live: true}},
		{"build with no commit", scoped(SourceDockerfile, ""), ok("cubeship-build/web/production/api:main", "")},
		{"pinned by digest", scoped(SourceExternal, "ghcr.io/acme/api"), ok("ghcr.io/acme/api@sha256:abc", "")},
		{"tag never resolved", scoped(SourceExternal, "ghcr.io/acme/api"), ok("1.4", "")},
		{"external image changed", scoped(SourceExternal, "ghcr.io/acme/web"), ok("ghcr.io/acme/api:1.4", "")},
		{"was built, now pulls", scoped(SourceRegistry, ""), ok("cubeship-build/web/production/api:main", sha)},
		{"another app's path", scoped(SourceRegistry, ""), ok(testRegistry+"/web/production/other:v1", "")},
	}
	for _, c := range refused {
		if _, _, err := rollbackTarget(c.app, c.d); !errors.Is(err, ErrNotRestorable) {
			t.Errorf("%s: got %v, want ErrNotRestorable", c.name, err)
		}
	}
}

// Builds are named for their branch, so a newer build on the branch
// takes the older commit's image name. Rolling back to the older commit
// must build it again rather than reuse a name that now holds the newer
// one.
func TestRollingBackToACommitWhoseImageWasReplacedBuildsIt(t *testing.T) {
	dbtest.RequireDatabase(t)
	builder := &fakeBuilder{}
	docker := &fakeDocker{nextCreateID: "c1", running: true}
	orch, db, a := newBuildingDeployFixture(t, docker, builder)
	ctx := context.Background()

	const older, newer = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccccccccccc"
	first := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{GitSHA: older, GitBranch: "main"})
	docker.nextCreateID = "c2"
	seedContainer(t, db, a.ID, "c1")
	second := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{GitSHA: newer, GitBranch: "main"})
	if first.Status != DeploymentSucceeded || second.Status != DeploymentSucceeded {
		t.Fatalf("setup deploys ended %q and %q", first.Status, second.Status)
	}
	if first.ImageRef != second.ImageRef {
		t.Fatalf("setup: the two builds were named %q and %q; this test needs one name", first.ImageRef, second.ImageRef)
	}

	scoped, err := NewRepository(db).ScopedByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	tag, identity, err := rollbackTarget(scoped, first)
	if err != nil {
		t.Fatalf("rollbackTarget: %v", err)
	}
	docker.nextCreateID = "c3"
	seedContainer(t, db, a.ID, "c2")
	back := runDeployWithIdentity(t, orch, db, a.ID, tag, identity)
	if back.Status != DeploymentSucceeded {
		t.Fatalf("rollback ended %q: %s", back.Status, back.Error)
	}
	if builder.count() != 3 {
		t.Errorf("built %d times, want 3: the older commit's image name was taken by the newer build", builder.count())
	}
	if !strings.HasSuffix(back.ImageRef, ":"+older) {
		t.Errorf("rollback ran %q, want an image named for commit %s", back.ImageRef, older)
	}
	if back.GitSHA != older || back.GitBranch != "main" {
		t.Errorf("rollback recorded %s on %q, want %s on main", back.GitSHA, back.GitBranch, older)
	}
}

// A commit whose image nothing has renamed since is not built again.
func TestRollingBackToACommitWhoseImageIsInPlaceReusesIt(t *testing.T) {
	dbtest.RequireDatabase(t)
	builder := &fakeBuilder{}
	docker := &fakeDocker{nextCreateID: "c1", running: true}
	orch, db, a := newBuildingDeployFixture(t, docker, builder)
	ctx := context.Background()

	const older, newer = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccccccccccc"
	first := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{GitSHA: older, GitBranch: "main"})
	docker.nextCreateID = "c2"
	seedContainer(t, db, a.ID, "c1")
	runDeployWithIdentity(t, orch, db, a.ID, "release", DeploymentIdentity{GitSHA: newer, GitBranch: "release"})

	scoped, err := NewRepository(db).ScopedByID(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	tag, identity, err := rollbackTarget(scoped, first)
	if err != nil {
		t.Fatalf("rollbackTarget: %v", err)
	}
	docker.nextCreateID = "c3"
	seedContainer(t, db, a.ID, "c2")
	back := runDeployWithIdentity(t, orch, db, a.ID, tag, identity)
	if back.Status != DeploymentSucceeded {
		t.Fatalf("rollback ended %q: %s", back.Status, back.Error)
	}
	if builder.count() != 2 {
		t.Errorf("built %d times, want 2: the older commit's image was still in place", builder.count())
	}
	if back.ImageRef != first.ImageRef {
		t.Errorf("rollback ran %q, want the first build %q", back.ImageRef, first.ImageRef)
	}
}

func TestSuccessfulImageForCommitSkipsAnImageANewerBuildRenamed(t *testing.T) {
	dbtest.RequireDatabase(t)
	ctx := context.Background()
	_, db, a := newDeployFixture(t, &fakeDocker{})
	repo := NewRepository(db)
	const image = "cubeship-build/web/production/myapp:main"
	record := func(sha string) {
		t.Helper()
		d, err := repo.StartDeploymentWithIdentity(ctx, a.ID, "main", sha, "main")
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.SetDeploymentImage(ctx, d.ID, image); err != nil {
			t.Fatal(err)
		}
		if err := repo.FinishDeployment(ctx, d.ID, DeploymentSucceeded, ""); err != nil {
			t.Fatal(err)
		}
	}
	const older, newer = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccccccccccccccccccc"
	record(older)
	record(newer)

	if got, err := repo.SuccessfulImageForCommit(ctx, a.ID, older); err != nil || got != "" {
		t.Errorf("older commit: got %q err %v, want nothing — %s now holds the newer build", got, err, image)
	}
	if got, err := repo.SuccessfulImageForCommit(ctx, a.ID, newer); err != nil || got != image {
		t.Errorf("newer commit: got %q err %v, want %q", got, err, image)
	}
}
