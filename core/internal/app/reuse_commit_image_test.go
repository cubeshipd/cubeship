package app

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/database/dbtest"
)

// fakeBuilder records how many times a Git deploy asked it to build, so
// a redeploy of the same commit can prove it skipped the build.
type fakeBuilder struct {
	mu    sync.Mutex
	calls int
}

func (b *fakeBuilder) Build(_ context.Context, _ buildkit.Request, logs io.Writer) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	_, _ = io.WriteString(logs, "built\n")
	return nil
}

func (b *fakeBuilder) BuildPlanned(_ context.Context, _ buildkit.PlannedRequest, logs io.Writer) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	_, _ = io.WriteString(logs, "built\n")
	return nil
}

func (b *fakeBuilder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

func newBuildingDeployFixture(t *testing.T, docker DockerAPI, builder ImageBuilder) (*Orchestrator, *database.DB, *App) {
	t.Helper()
	orch, db, registryApp := newDeployFixture(t, docker)
	ctx := context.Background()
	src := SourceDockerfile
	a, err := NewRepository(db).Update(ctx, registryApp.ID, &src, &Origin{
		Repo: "https://github.com/acme/api.git", Ref: "main",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("turn the app into a building one: %v", err)
	}
	orch.builder = builder
	return orch, db, a
}

func runDeployWithIdentity(t *testing.T, orch *Orchestrator, db *database.DB, appID int64, tag string, identity DeploymentIdentity) *Deployment {
	t.Helper()
	ctx := context.Background()
	deployment, err := orch.StartWithIdentity(ctx, appID, tag, identity)
	if err != nil {
		t.Fatalf("StartWithIdentity: %v", err)
	}
	orch.Wait()
	finished, err := NewRepository(db).DeploymentByID(ctx, appID, deployment.ID)
	if err != nil {
		t.Fatalf("read the deployment back: %v", err)
	}
	return finished
}

// Pressing Deploy on a commit this app already built must not build
// again: the image is reused and the container still starts with the
// env as it is now.
func TestRedeployingTheSameGitCommitReusesTheBuiltImage(t *testing.T) {
	dbtest.RequireDatabase(t)
	builder := &fakeBuilder{}
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newBuildingDeployFixture(t, docker, builder)

	const sha = "a8ef4fa0123456789abcdef0123456789abcdef0"
	identity := DeploymentIdentity{GitSHA: sha, GitBranch: "main"}

	first := runDeployWithIdentity(t, orch, db, a.ID, "main", identity)
	if first.Status != DeploymentSucceeded {
		t.Fatalf("the first deploy ended %q: %s", first.Status, first.Error)
	}
	if builder.count() != 1 {
		t.Fatalf("the first deploy built %d times, want 1", builder.count())
	}
	if !resolvedImage(first.ImageRef) {
		t.Fatalf("the first deploy recorded %q, which is not a built image", first.ImageRef)
	}

	docker.nextCreateID = "redeployed-container"
	seedContainer(t, db, a.ID, "old-container")

	second := runDeployWithIdentity(t, orch, db, a.ID, "main", identity)
	if second.Status != DeploymentSucceeded {
		t.Fatalf("the redeploy ended %q: %s", second.Status, second.Error)
	}
	if builder.count() != 1 {
		t.Fatalf("redeploying the same commit built again (%d builds); want the image reused", builder.count())
	}
	if second.ImageRef != first.ImageRef {
		t.Errorf("redeployed %q, want the first image %q", second.ImageRef, first.ImageRef)
	}
	if !strings.Contains(second.Logs, "already built") {
		t.Errorf("the redeploy log does not say the image was reused: %q", second.Logs)
	}
	if second.GitSHA != sha {
		t.Errorf("redeploy recorded git sha %q, want %q", second.GitSHA, sha)
	}
}

// A commit with no successful image still builds — a failed attempt, or
// nothing at all, is not something to reuse.
func TestRedeployingACommitWithNoSuccessfulImageStillBuilds(t *testing.T) {
	dbtest.RequireDatabase(t)
	builder := &fakeBuilder{}
	docker := &fakeDocker{nextCreateID: "new-container", running: true}
	orch, db, a := newBuildingDeployFixture(t, docker, builder)
	ctx := context.Background()
	repo := NewRepository(db)

	const sha = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	failed, err := repo.StartDeploymentWithIdentity(ctx, a.ID, "main", sha, "main")
	if err != nil {
		t.Fatalf("record a failed deploy: %v", err)
	}
	if err := repo.FinishDeployment(ctx, failed.ID, DeploymentFailed, "the build blew up"); err != nil {
		t.Fatalf("finish the failed deploy: %v", err)
	}

	d := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{GitSHA: sha, GitBranch: "main"})
	if d.Status != DeploymentSucceeded {
		t.Fatalf("deploy ended %q: %s", d.Status, d.Error)
	}
	if builder.count() != 1 {
		t.Fatalf("built %d times, want 1 — a failed deploy is not an image to reuse", builder.count())
	}
	if !resolvedImage(d.ImageRef) {
		t.Errorf("deploy recorded %q, want a built image", d.ImageRef)
	}
}

// A different commit is a different artifact, even when the branch name
// is the same.
func TestDeployingADifferentGitCommitBuildsAgain(t *testing.T) {
	dbtest.RequireDatabase(t)
	builder := &fakeBuilder{}
	docker := &fakeDocker{nextCreateID: "c1", running: true}
	orch, db, a := newBuildingDeployFixture(t, docker, builder)

	first := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{
		GitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GitBranch: "main",
	})
	if first.Status != DeploymentSucceeded {
		t.Fatalf("first deploy: %s", first.Error)
	}

	docker.nextCreateID = "c2"
	second := runDeployWithIdentity(t, orch, db, a.ID, "main", DeploymentIdentity{
		GitSHA: "cccccccccccccccccccccccccccccccccccccccc", GitBranch: "main",
	})
	if second.Status != DeploymentSucceeded {
		t.Fatalf("second deploy: %s", second.Error)
	}
	if builder.count() != 2 {
		t.Fatalf("built %d times, want 2 for two different commits", builder.count())
	}
}

func TestSuccessfulImageForCommit(t *testing.T) {
	dbtest.RequireDatabase(t)
	ctx := context.Background()
	_, db, a := newDeployFixture(t, &fakeDocker{})
	repo := NewRepository(db)

	const sha = "dddddddddddddddddddddddddddddddddddddddd"
	got, err := repo.SuccessfulImageForCommit(ctx, a.ID, sha)
	if err != nil || got != "" {
		t.Fatalf("no history: got %q err %v", got, err)
	}

	tagOnly, err := repo.StartDeploymentWithIdentity(ctx, a.ID, "main", sha, "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishDeployment(ctx, tagOnly.ID, DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	got, err = repo.SuccessfulImageForCommit(ctx, a.ID, sha)
	if err != nil || got != "" {
		t.Fatalf("a success that never recorded an image: got %q err %v", got, err)
	}

	built, err := repo.StartDeploymentWithIdentity(ctx, a.ID, "main", sha, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := "cubeship-build/web/production/myapp:main"
	if err := repo.SetDeploymentImage(ctx, built.ID, want); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishDeployment(ctx, built.ID, DeploymentSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	got, err = repo.SuccessfulImageForCommit(ctx, a.ID, sha)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
