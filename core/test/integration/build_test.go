//go:build integration

// Building an app from a repository, on a real Linux Docker daemon.
//
// This is the only place the daemon's own path to buildkitd can be
// proven. It reaches it over a unix socket in a bind mount, and Docker
// Desktop refuses to create a socket there — so the unit tests in
// internal/platform/buildkit reach a buildkitd over TCP instead, and
// everything below the dial goes untested until here.

package integration

import (
	"fmt"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cubeship/internal/platform/httpx"
)

// buildFromARepository runs inside TestDeployEndToEnd, which already has
// a daemon, an organization and a project. A second daemon would want
// the same port and the same infrastructure containers as the first.
func buildFromARepository(t *testing.T, apiKey string) {
	repo := serveRepo(t, map[string]string{
		"Dockerfile": "FROM busybox\n" +
			"RUN echo '<h1>built by cubeship</h1>' > /index.html\n" +
			"CMD [\"httpd\", \"-f\", \"-p\", \"8080\", \"-h\", \"/\"]\n",
	})

	created := createApp(t, apiKey, map[string]string{
		"name": "built", "domain": "built.localtest.me",
		"project": "web",
		"source":  "dockerfile", "repo": repo, "ref": "main",
	})

	deployment := deployApp(t, apiKey, created)

	// A build takes minutes on a cold cache: the builder's image has to
	// be pulled before anything else happens.
	waitFor(t, 10*time.Minute, "the build to finish", func() bool {
		return deploymentStatus(t, apiKey, created, deployment) != "pending"
	})

	status := deploymentStatus(t, apiKey, created, deployment)
	if status != "succeeded" {
		t.Fatalf("the build ended %q:\n%s", status, deploymentLogs(t, apiKey, created, deployment))
	}

	// The build's output reached the row while it ran, which is what
	// makes a build watchable rather than a blank wait.
	if logs := deploymentLogs(t, apiKey, created, deployment); !strings.Contains(logs, "busybox") {
		t.Errorf("the build's output is missing from the deployment:\n%s", logs)
	}

	// And the image it produced is running, without ever having been
	// pushed to or pulled from a registry.
	waitFor(t, 2*time.Minute, "the built app to be running", func() bool {
		return appStatus(t, apiKey, created) == "running"
	})
}

// Railpack, on the same daemon: no Dockerfile anywhere in the
// repository, and Cubeship works the build out from the code.
func buildWithRailpack(t *testing.T, apiKey string) {
	repo := serveRepo(t, map[string]string{
		"package.json": `{"name":"demo","version":"1.0.0","scripts":{"start":"node index.js"}}`,
		"index.js":     `require("http").createServer((_,r)=>r.end("built by railpack")).listen(8080)`,
	})
	created := createApp(t, apiKey, map[string]string{
		"name": "detected", "domain": "detected.localtest.me",
		"project": "web",
		"source":  "railpack", "repo": repo, "ref": "main",
	})
	id := deployApp(t, apiKey, created)

	waitFor(t, 15*time.Minute, "the railpack build to finish", func() bool {
		return deploymentStatus(t, apiKey, created, id) != "pending"
	})
	if status := deploymentStatus(t, apiKey, created, id); status != "succeeded" {
		t.Fatalf("the build ended %q:\n%s", status, deploymentLogs(t, apiKey, created, id))
	}

	// It said what it recognized, which is the one thing a Railpack
	// build's log has that a Dockerfile's does not.
	if logs := deploymentLogs(t, apiKey, created, id); !strings.Contains(logs, "Detected") {
		t.Errorf("the log does not say what was detected:\n%s", logs)
	}

	waitFor(t, 2*time.Minute, "the built app to be running", func() bool {
		return appStatus(t, apiKey, created) == "running"
	})
}

// A build is the one part of a deploy long enough that watching it is
// the point, so its output has to reach the row while it is still
// running rather than all at once at the end.
func buildLogsArriveWhileTheBuildRuns(t *testing.T, apiKey string) {
	repo := serveRepo(t, map[string]string{
		"Dockerfile": "FROM busybox\n" +
			"RUN for i in 1 2 3 4 5 6 7 8; do echo step-$i; sleep 2; done\n",
	})
	created := createApp(t, apiKey, map[string]string{
		"name": "watched", "domain": "watched.localtest.me",
		"project": "web",
		"source":  "dockerfile", "repo": repo, "ref": "main",
	})
	id := deployApp(t, apiKey, created)

	waitFor(t, 10*time.Minute, "output to appear before the build ends", func() bool {
		status, logs := deployment(t, apiKey, created, id)
		return logs != "" && status == "pending"
	})
}

// serveRepo publishes a repository over Git's dumb HTTP protocol, which
// needs no Git server — only static files and update-server-info.
func serveRepo(t *testing.T, files map[string]string) string {
	t.Helper()

	work := t.TempDir()
	for name, content := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=cubeship", "GIT_AUTHOR_EMAIL=test@cubeship.invalid",
			"GIT_COMMITTER_NAME=cubeship", "GIT_COMMITTER_EMAIL=test@cubeship.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(work, "init", "--quiet", "--initial-branch=main")
	git(work, "add", ".")
	git(work, "commit", "--quiet", "-m", "initial")

	serveDir := t.TempDir()
	bare := filepath.Join(serveDir, "repo.git")
	git(work, "clone", "--quiet", "--bare", work, bare)
	git(bare, "--git-dir", bare, "update-server-info")

	// Served over git's smart protocol: BuildKit's git would cope with a
	// plain file server, but the daemon clones with go-git for a
	// Railpack build, and go-git speaks smart HTTP only.
	backend := filepath.Join(strings.TrimSpace(gitExecPath(t)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Fatalf("git-http-backend is not installed: %v", err)
	}
	handler := &cgi.Handler{
		Path: backend,
		Env:  []string{"GIT_PROJECT_ROOT=" + serveDir, "GIT_HTTP_EXPORT_ALL=1"},
	}

	// The builder is a container reaching back to this process, and a
	// Railpack build clones in the daemon: see hostAddress.
	l, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return "http://" + hostAddress(t) + ":" + port + "/repo.git"
}

func apiRequest(t *testing.T, method, path, apiKey string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, daemonURL+httpx.APIPrefix+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// createApp creates an app and, when fields names a "domain", adds it
// afterwards: an app is created empty and a domain is its own resource,
// and without one a deploy is refused.
func createApp(t *testing.T, apiKey string, fields map[string]string) string {
	t.Helper()
	domain := fields["domain"]
	var parts []string
	for k, v := range fields {
		if k != "domain" {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
	}
	resp := apiRequest(t, http.MethodPost, "/apps", apiKey, "{"+strings.Join(parts, ",")+"}")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create app: %s", resp.Status)
	}
	var created struct {
		Reference string `json:"reference"`
	}
	jsonDecodeOrFatal(t, resp, &created)

	if domain != "" {
		resp := apiRequest(t, http.MethodPost, "/apps/"+created.Reference+"/domains", apiKey,
			fmt.Sprintf(`{"host":%q}`, domain))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			t.Fatalf("add domain: %s", resp.Status)
		}
	}
	return created.Reference
}

func deployApp(t *testing.T, apiKey, reference string) int64 {
	t.Helper()
	resp := apiRequest(t, http.MethodPost, "/apps/"+reference+"/deploy", apiKey, "{}")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("deploy: %s", resp.Status)
	}
	var d struct {
		ID int64 `json:"id"`
	}
	jsonDecodeOrFatal(t, resp, &d)
	return d.ID
}

func deployment(t *testing.T, apiKey, reference string, id int64) (status, logs string) {
	t.Helper()
	resp := apiRequest(t, http.MethodGet,
		fmt.Sprintf("/apps/%s/deployments/%d", reference, id), apiKey, "")
	defer resp.Body.Close()
	var d struct {
		Status string `json:"status"`
		Logs   string `json:"logs"`
	}
	jsonDecodeOrFatal(t, resp, &d)
	return d.Status, d.Logs
}

func deploymentStatus(t *testing.T, apiKey, reference string, id int64) string {
	t.Helper()
	status, _ := deployment(t, apiKey, reference, id)
	return status
}

func deploymentLogs(t *testing.T, apiKey, reference string, id int64) string {
	t.Helper()
	_, logs := deployment(t, apiKey, reference, id)
	return logs
}

func appStatus(t *testing.T, apiKey, reference string) string {
	t.Helper()
	resp := apiRequest(t, http.MethodGet, "/apps/"+reference, apiKey, "")
	defer resp.Body.Close()
	var a struct {
		Status string `json:"status"`
	}
	jsonDecodeOrFatal(t, resp, &a)
	return a.Status
}

func gitExecPath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("git --exec-path: %v", err)
	}
	return string(out)
}
