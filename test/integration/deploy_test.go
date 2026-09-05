//go:build integration

// This test needs a real Linux host to fully pass. Traefik is bootstrapped
// with Docker's --network host (see bootstrap.TraefikContainerOpts) so it
// can reach both the host-process daemon and other containers — that's a
// real, working mode on the Linux Docker daemon a production VPS runs, but
// on Docker Desktop for Mac/Windows the Docker Engine runs inside a VM and
// host networking is NOT bridged out to the physical machine: a container
// started with --network host binds ports inside that VM only, invisible
// to `lsof` or connections from the Mac/Windows host itself. Concretely:
// this test's app-reachable-via-Traefik step (and any docker push to
// registry.<domain> through Traefik on :443) cannot succeed on Docker
// Desktop for that reason — confirmed by direct observation (nothing
// listens on :443 on the host despite Traefik running and reporting
// healthy).
//
// The test therefore avoids Traefik for everything before that last hop:
// it logs in and pushes to the registry's loopback publication
// (localhost:5000, which Docker trusts as insecure-by-default), and the
// daemon pulls from 127.0.0.1:5000 rather than registry.<domain>, so no
// certificate has to exist for a deploy to happen. Only the final
// HTTPS-through-Traefik assertion needs Linux.
//
// LAST CONFIRMED on Docker Desktop for Mac (2026-09-04, before the
// registry moved from a shared htpasswd credential to per-user Docker
// Registry v2 token auth — see internal/regauth): the daemon starts,
// POST /setup claims the instance and issues an API key, the registry
// and Traefik bootstrap, org creation,
// org-scoped app creation, `docker login` + `docker push` to
// localhost:5000/<org>/<project>/<env>/<app> succeed, the registry's push notification
// fires the webhook, and the daemon pulls and deploys the app
// successfully — the test got all the way to and failed only at the
// final "app reachable via Traefik" assertion, exactly the --network
// host limitation described above and nothing earlier.
//
// RE-CONFIRMED FAILS EARLIER on Docker Desktop for Mac since the
// token-auth switch, for the same underlying reason as the final-step
// limitation above. The registry's config.yml points its token realm at
// https://api.<domain>/v2/token — reached through Traefik on :443, same
// as the app-reachability assertion — so on Docker Desktop, `docker
// login` itself now fails before it can even fetch a token, well before
// the push/webhook/deploy steps this test exercises. On a real Linux
// VPS (Traefik's --network host actually bridging, as intended) this
// realm is correct and necessary: a real remote push from someone's own
// machine has no other way to reach the daemon's token endpoint.
//
// The authorization logic itself (which org membership grants access to
// which repository scope) is fully covered by real JWT-verifying unit
// tests in internal/api/token_handler_test.go — this integration test
// currently cannot exercise anything past `docker login` on this
// platform. Re-verify on a real Linux host if this note needs updating.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cubeship/internal/cli/client"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/user"
)

// daemonURL is the plaintext address the daemon publishes for the
// registry container's webhook; the test reaches it the same way.
const daemonURL = "http://127.0.0.1:3000"

const testToken = "integration-test-token"

func waitFor(t *testing.T, timeout time.Duration, desc string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func TestDeployEndToEnd(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	daemonBin := filepath.Join(t.TempDir(), "cubeshipd")
	build := exec.Command("go", "build", "-o", daemonBin, "./cmd/cubeshipd")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build daemon: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	daemon := exec.Command(daemonBin)
	daemon.Env = append(os.Environ(),
		"CUBESHIP_DOMAIN=localtest.me",
		"CUBESHIP_ACME_EMAIL=test@example.com",
		"CUBESHIP_TOKEN="+testToken,
		"CUBESHIP_DATA_DIR="+dataDir,
	)
	daemon.Stdout = os.Stdout
	daemon.Stderr = os.Stderr
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		daemon.Process.Kill()
		daemon.Wait()
		exec.Command("docker", "rm", "-f", "cubeship-registry", "cubeship-traefik").Run()
		exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter name=cubeship-myapp-)").Run()
	})

	waitFor(t, 30*time.Second, "daemon healthz", func() bool {
		resp, err := http.Get(daemonURL + "/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	waitFor(t, 60*time.Second, "registry reachable on localhost:5000", func() bool {
		resp, err := http.Get("http://127.0.0.1:5000/v2/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		// The registry now requires basic auth, so an anonymous /v2/
		// probe gets 401 — which still proves it is up and serving.
		return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
	})

	ctx := context.Background()
	// CUBESHIP_TOKEN is the registry/webhook credential only. A fresh
	// instance has no account at all: the first request anyone makes
	// claims it, exactly as a browser would on the setup page.
	adminKey := claimInstance(t, adminUsername, adminPassword)
	client := client.New(daemonURL, adminKey)

	if _, err := client.CreateProject(ctx, "web"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	created, err := client.CreateApp(ctx, "myapp", "web", "production", "")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	// A name to answer at, with its port read from the image.
	if _, err := client.AddAppDomain(ctx, created.Reference, "myapp.localtest.me", 0); err != nil {
		t.Fatalf("AddAppDomain: %v", err)
	}
	// The push path is the app's reference with the registry host in
	// front — project, environment, name.
	if created.Reference != "web/production/myapp" {
		t.Fatalf("unexpected reference: %q", created.Reference)
	}
	if created.Image != "registry.localtest.me/"+created.Reference {
		t.Fatalf("unexpected image: %q", created.Image)
	}

	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/web/production/myapp:latest", "./testapp")
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}

	// The registry rejects anonymous pushes and grants access per the
	// pushing user's org membership — the super-admin, who claimed this
	// instance, is authorized everywhere. This is what
	// `cubeship registry login` does for any real user with their own
	// username and API key.
	login := exec.Command("docker", "login", "localhost:5000", "-u", adminUsername, "--password-stdin")
	login.Stdin = strings.NewReader(adminKey)
	if out, err := login.CombinedOutput(); err != nil {
		t.Fatalf("docker login to the local registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "logout", "localhost:5000").Run() })

	push := exec.Command("docker", "push", "localhost:5000/web/production/myapp:latest")
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push fixture image: %v\n%s", err, out)
	}

	waitFor(t, 60*time.Second, "app deployed after push", func() bool {
		req, _ := http.NewRequest(http.MethodGet, daemonURL+httpx.APIPrefix+"/apps/myapp", nil)
		req.Header.Set("Authorization", "Bearer "+adminKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var app struct {
			Status string `json:"status"`
		}
		if resp.StatusCode != http.StatusOK {
			return false
		}
		jsonDecodeOrFatal(t, resp, &app)
		return app.Status == "running"
	})

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   10 * time.Second,
	}

	var body []byte
	waitFor(t, 30*time.Second, "app reachable via Traefik", func() bool {
		resp, err := httpClient.Get("https://myapp.localtest.me/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		body = buf[:n]
		return resp.StatusCode == http.StatusOK
	})

	if string(body) != "hello from cubeship\n" {
		t.Fatalf("unexpected response body from the deployed app: %q", body)
	}

	// Building runs on the same daemon, and needs the organization and
	// project this test already made. A second daemon would want the
	// same port and the same infrastructure containers as this one.
	t.Run("builds an app from a repository", func(t *testing.T) {
		buildFromARepository(t, adminKey)
	})
	t.Run("builds an app with no Dockerfile at all", func(t *testing.T) {
		buildWithRailpack(t, adminKey)
	})
	t.Run("a build's output arrives while it runs", func(t *testing.T) {
		buildLogsArriveWhileTheBuildRuns(t, adminKey)
	})
}

const (
	adminUsername = "admin"
	adminPassword = "integration-test-password"
)

// claimInstance walks the first-run flow: POST /setup creates the only
// account this instance will ever hand out, and signs the caller in. The
// session that comes back then issues the API key everything else here
// authenticates with — a browser can hold a cookie, `docker login` and
// the CLI cannot.
func claimInstance(t *testing.T, username, password string) string {
	t.Helper()

	post := func(path string, body any, cookie *http.Cookie) *http.Response {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, daemonURL+httpx.APIPrefix+path,
			bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		return resp
	}

	resp := post("/setup", map[string]string{"username": username, "password": password}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /setup: %s", resp.Status)
	}

	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == user.SessionCookieName {
			session = c
		}
	}
	if session == nil {
		t.Fatal("setup did not return a session cookie")
	}

	keyResp := post("/users/me/api-keys", map[string]string{"name": "integration"}, session)
	defer keyResp.Body.Close()
	if keyResp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /users/me/api-keys: %s", keyResp.Status)
	}
	var created struct {
		Key string `json:"key"`
	}
	jsonDecodeOrFatal(t, keyResp, &created)
	if created.Key == "" {
		t.Fatal("no api key in the response")
	}
	return created.Key
}

func jsonDecode(resp *http.Response, v any) error {
	return json.NewDecoder(resp.Body).Decode(v)
}

func jsonDecodeOrFatal(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := jsonDecode(resp, v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
