package buildkit_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cubeship/internal/platform/buildkit"
)

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

	// A deadline of its own. The buildkit client retries a dial for 90
	// seconds before giving up, and what this test is about is the error
	// it ends with, not how long it is willing to wait for it.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := b.Build(ctx, buildkit.Request{
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

// initRepo makes a directory into a Git repository with one commit on
// main. git is a normal thing to have installed; nothing here reaches
// the network.
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

// The bug this exists for: every build of a private GitHub repository
// failed with
//
//	failed to read downloaded context: failed to load cache key: invalid response status 404
//
// BuildKit reads an https context as a repository **only** when its
// path ends in `.git`. Without the suffix it downloaded the URL as a
// tarball, with none of the credentials a clone would carry — so what
// came back for a private repository was GitHub's 404, and for a public
// one an HTML page. Nothing in the message says either of those things.
func TestAnHTTPSContextIsGivenTheSuffixBuildKitLooksFor(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/api":           "https://github.com/acme/api.git",
		"https://github.com/acme/api#main":      "https://github.com/acme/api.git#main",
		"https://github.com/acme/api/":          "https://github.com/acme/api.git",
		"http://git.internal/acme/api":          "http://git.internal/acme/api.git",
		"https://gitlab.com/group/sub/api#v1.2": "https://gitlab.com/group/sub/api.git#v1.2",

		// Already a repository as far as BuildKit is concerned.
		"https://github.com/acme/api.git":      "https://github.com/acme/api.git",
		"https://github.com/acme/api.git#main": "https://github.com/acme/api.git#main",
		"https://github.com/acme/API.GIT":      "https://github.com/acme/API.GIT",

		// Schemes that are a git transport on their own. Adding the
		// suffix to one of these would change which repository it is.
		"git://git.internal/acme/api":   "git://git.internal/acme/api",
		"ssh://git@github.com/acme/api": "ssh://git@github.com/acme/api",
		"git@github.com:acme/api":       "git@github.com:acme/api",
	}
	for in, want := range cases {
		if got := buildkit.GitContext(in); got != want {
			t.Errorf("buildkit.GitContext(%q) = %q, want %q", in, got, want)
		}
	}
}

// A ref is a fragment on the URL, and the suffix goes before it. Put
// after, `#main.git` is a branch nobody has.
func TestTheRefStaysAFragment(t *testing.T) {
	got := buildkit.GitContext("https://github.com/acme/api#feature/some-branch")
	if want := "https://github.com/acme/api.git#feature/some-branch"; got != want {
		t.Errorf("GitContext = %q, want %q", got, want)
	}
}
