package buildkit_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cubeship/internal/platform/buildkit"
	"cubeship/internal/platform/dockerx"

	"github.com/moby/buildkit/client"
)

// A real buildkitd, reached over a loopback port.
//
// The daemon reaches it over a unix socket instead — see
// bootstrap.BuildKitSocket — because a build service running as root
// with no authentication of its own is better guarded by filesystem
// permissions than by a port. That transport cannot be used here:
// Docker Desktop's file sharing refuses to create a socket in a bind
// mount, so a test on a Mac would only ever prove the Mac's limits.
// Everything above the dial is the same either way.
func buildkitd(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("this test needs Docker; skipping would let `make check` pass for a build path nobody ran")
	}

	name := fmt.Sprintf("cubeship-buildkit-test-%d", time.Now().UnixNano())
	port := freePort(t)
	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"--privileged", "-p", fmt.Sprintf("127.0.0.1:%d:1234", port),
		"--add-host", "host.docker.internal:host-gateway",
		buildkitImage, "--addr", "tcp://0.0.0.0:1234")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("start buildkitd: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })

	// Probed with the same client the daemon uses, rather than with
	// buildctl inside the container — buildctl defaults to the unix
	// socket, so it would report a healthy TCP buildkitd as unreachable.
	addr := fmt.Sprintf("tcp://127.0.0.1:%d", port)
	waitFor(t, 90*time.Second, "buildkitd accepting connections", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c, err := client.New(ctx, addr)
		if err != nil {
			return false
		}
		defer c.Close()
		workers, err := c.ListWorkers(ctx)
		return err == nil && len(workers) > 0
	})
	return addr
}

const buildkitImage = "moby/buildkit:v0.32.2"

// initRepo makes a directory into a Git repository with one commit on
// main.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("this test needs git")
	}
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"add", "."},
		{"commit", "--quiet", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=cubeship", "GIT_AUTHOR_EMAIL=test@cubeship.invalid",
			"GIT_COMMITTER_NAME=cubeship", "GIT_COMMITTER_EMAIL=test@cubeship.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitFor(t *testing.T, limit time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// source writes a build context and returns its directory.
func source(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The whole point: source in, a runnable image out, with no registry in
// between.
func TestBuildProducesAnImageTheEngineCanRun(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatalf("connect to Docker: %v", err)
	}
	b := buildkit.New(buildkitd(t), docker)

	image := fmt.Sprintf("cubeship-test/hello:%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rmi", "-f", image).Run() })

	dir := source(t, map[string]string{
		"Dockerfile": "FROM busybox\nCOPY greeting .\nCMD [\"cat\", \"greeting\"]\n",
		"greeting":   "hello from a build\n",
	})

	var logs bytes.Buffer
	if err := b.Build(context.Background(), buildkit.Request{
		ContextDir: dir, Image: image,
	}, &logs); err != nil {
		t.Fatalf("Build: %v\n%s", err, logs.String())
	}

	// The image is in the Engine's store, not somewhere that needed a
	// push and a pull to get there.
	out, err := exec.Command("docker", "run", "--rm", image).CombinedOutput()
	if err != nil {
		t.Fatalf("run the built image: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "hello from a build" {
		t.Errorf("the built image printed %q", got)
	}
	if logs.Len() == 0 {
		t.Error("the build produced no log output at all")
	}
}

// A failing build has to fail, and say why. BuildKit reports a step's
// failure inside its status stream, so a builder that only watched the
// transport would call this a success.
func TestAFailedBuildFailsWithItsOutput(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	b := buildkit.New(buildkitd(t), docker)

	dir := source(t, map[string]string{
		"Dockerfile": "FROM busybox\nRUN echo the-reason-it-failed && exit 3\n",
	})

	var logs bytes.Buffer
	err = b.Build(context.Background(), buildkit.Request{
		ContextDir: dir, Image: "cubeship-test/broken:latest",
	}, &logs)
	if err == nil {
		t.Fatal("a build whose step exited non-zero was reported as a success")
	}
	if !strings.Contains(logs.String(), "the-reason-it-failed") {
		t.Errorf("the build's own output is missing from the logs:\n%s", logs.String())
	}
}

// Build args reach the Dockerfile. Without them nothing configurable can
// be built.
func TestBuildArgsReachTheDockerfile(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	b := buildkit.New(buildkitd(t), docker)

	image := fmt.Sprintf("cubeship-test/args:%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rmi", "-f", image).Run() })

	dir := source(t, map[string]string{
		"Dockerfile": "FROM busybox\nARG GREETING\nRUN echo \"$GREETING\" > /greeting\nCMD [\"cat\", \"/greeting\"]\n",
	})

	var logs bytes.Buffer
	if err := b.Build(context.Background(), buildkit.Request{
		ContextDir: dir, Image: image, Args: map[string]string{"GREETING": "from an arg"},
	}, &logs); err != nil {
		t.Fatalf("Build: %v\n%s", err, logs.String())
	}

	out, err := exec.Command("docker", "run", "--rm", image).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "from an arg" {
		t.Errorf("the built image printed %q", got)
	}
}

// A missing recipe is refused before buildkitd is dialled: it is the
// commonest mistake, and the answer should name the file rather than
// arrive as a solve error.
func TestAMissingDockerfileIsRefusedEarly(t *testing.T) {
	b := buildkit.New("tcp://127.0.0.1:1", nopLoader{})

	err := b.Build(context.Background(), buildkit.Request{
		ContextDir: source(t, map[string]string{"main.go": "package main"}),
		Image:      "cubeship-test/nothing:latest",
	}, nil)
	if err == nil {
		t.Fatal("a source with no Dockerfile was accepted")
	}
	if !strings.Contains(err.Error(), "Dockerfile") {
		t.Errorf("the error does not name the missing file: %v", err)
	}
}

type nopLoader struct{}

func (nopLoader) LoadImage(context.Context, io.Reader) error { return nil }

// A build with no builder behind it has to say so. The message reaches
// a deployment row and then a browser, and "connection refused" from a
// socket path nobody has heard of explains nothing.
func TestAnUnreachableBuilderSaysSo(t *testing.T) {
	b := buildkit.New("tcp://127.0.0.1:1", nopLoader{})

	err := b.Build(context.Background(), buildkit.Request{
		ContextDir: source(t, map[string]string{"Dockerfile": "FROM busybox\n"}),
		Image:      "cubeship-test/nowhere:latest",
	}, nil)
	if err == nil {
		t.Fatal("a build with no builder was reported as a success")
	}
	if !errors.Is(err, buildkit.ErrUnavailable) {
		t.Errorf("got %v, want it to be ErrUnavailable so the deploy path can explain it", err)
	}
}

// localRepoPath is a repository on disk, for a clone that does not go
// over the network.
func localRepoPath(t *testing.T, files map[string]string) string {
	t.Helper()
	work := source(t, files)
	initRepo(t, work)
	return work
}

// gitRepo makes a repository BuildKit can clone, served over Git's dumb
// HTTP protocol — static files out of a bare repo, which needs no git
// server, only `git update-server-info`.
//
// A real third-party URL would make this test depend on someone else's
// uptime and someone else's repository staying as it is.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("this test needs git")
	}

	work := source(t, files)
	initRepo(t, work)

	bare := filepath.Join(t.TempDir(), "repo.git")
	if out, err := exec.Command("git", "clone", "--quiet", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "--git-dir", bare, "update-server-info").CombinedOutput(); err != nil {
		t.Fatalf("update-server-info: %v\n%s", err, out)
	}

	srv := httptest.NewServer(http.FileServer(http.Dir(filepath.Dir(bare))))
	t.Cleanup(srv.Close)

	// The builder is a container, so it reaches this server by the
	// host's gateway rather than by the loopback the test sees.
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return "http://host.docker.internal:" + port + "/repo.git"
}

// Building from a repository, which is the whole of what a Dockerfile
// app does: BuildKit clones it itself, so nothing here needs git on the
// host or a working copy on disk.
func TestBuildFromAGitRepository(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	b := buildkit.New(buildkitd(t), docker)

	repo := gitRepo(t, map[string]string{
		"Dockerfile": "FROM busybox\nCOPY greeting .\nCMD [\"cat\", \"greeting\"]\n",
		"greeting":   "hello from a clone\n",
	})

	image := fmt.Sprintf("cubeship-test/cloned:%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rmi", "-f", image).Run() })

	var logs bytes.Buffer
	if err := b.Build(context.Background(), buildkit.Request{
		ContextGit: repo + "#main", Image: image,
	}, &logs); err != nil {
		t.Fatalf("Build: %v\n%s", err, logs.String())
	}

	out, err := exec.Command("docker", "run", "--rm", image).CombinedOutput()
	if err != nil {
		t.Fatalf("run the built image: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "hello from a clone" {
		t.Errorf("the built image printed %q", got)
	}
}

// A Dockerfile somewhere other than the root, which is what a monorepo
// needs.
func TestBuildFromANestedDockerfile(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	b := buildkit.New(buildkitd(t), docker)

	repo := gitRepo(t, map[string]string{
		"services/api/Dockerfile": "FROM busybox\nCOPY services/api/greeting .\nCMD [\"cat\", \"greeting\"]\n",
		"services/api/greeting":   "hello from a subdirectory\n",
	})

	image := fmt.Sprintf("cubeship-test/nested:%d", time.Now().UnixNano())
	t.Cleanup(func() { exec.Command("docker", "rmi", "-f", image).Run() })

	var logs bytes.Buffer
	if err := b.Build(context.Background(), buildkit.Request{
		ContextGit: repo + "#main", Dockerfile: "services/api/Dockerfile", Image: image,
	}, &logs); err != nil {
		t.Fatalf("Build: %v\n%s", err, logs.String())
	}

	out, err := exec.Command("docker", "run", "--rm", image).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "hello from a subdirectory" {
		t.Errorf("the built image printed %q", got)
	}
}
