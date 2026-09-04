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
// bootstraps a super-admin with its own key (separate from
// CUBESHIP_TOKEN), the registry and Traefik bootstrap, org creation,
// org-scoped app creation, `docker login` + `docker push` to
// localhost:5000/<org>/<app> succeed, the registry's push notification
// fires the webhook, and the daemon pulls and deploys the app
// successfully — the test got all the way to and failed only at the
// final "app reachable via Traefik" assertion, exactly the --network
// host limitation described above and nothing earlier.
//
// NOT YET RE-CONFIRMED since the token-auth switch. The login step now
// authenticates as the super-admin's real username ("admin") against
// the registry's token realm (GET /v2/token) instead of a fixed
// htpasswd account — that path is covered by unit tests
// (internal/api/token_handler_test.go) but not by a live run against
// real Docker yet. Re-verify this note (and update it) the next time
// this test is run live.
package integration

import (
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

	"cubeship/internal/apiclient"
)

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
		resp, err := http.Get("http://127.0.0.1:9000/healthz")
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
	// CUBESHIP_TOKEN is the registry/webhook credential only; the
	// super-admin's API key is generated on first boot and persisted
	// under the data dir, so that is what talks to the daemon API.
	adminKey := readAdminAPIKey(t, dataDir)
	client := apiclient.New("http://127.0.0.1:9000", adminKey)

	if err := client.CreateOrg(ctx, "acme", "Acme Inc"); err != nil {
		t.Fatalf("CreateOrg: %v", err)
	}

	image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me", "acme")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.localtest.me/acme/myapp" {
		t.Fatalf("unexpected image: %q", image)
	}

	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/acme/myapp:latest", "./testapp")
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}

	// The registry rejects anonymous pushes and grants access per the
	// pushing user's org membership — the super-admin ("admin", the
	// hardcoded bootstrap username) is authorized everywhere, same as
	// `cubeship registry login` would do for any real user with their
	// own username and API key.
	login := exec.Command("docker", "login", "localhost:5000", "-u", "admin", "--password-stdin")
	login.Stdin = strings.NewReader(adminKey)
	if out, err := login.CombinedOutput(); err != nil {
		t.Fatalf("docker login to the local registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "logout", "localhost:5000").Run() })

	push := exec.Command("docker", "push", "localhost:5000/acme/myapp:latest")
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push fixture image: %v\n%s", err, out)
	}

	waitFor(t, 60*time.Second, "app deployed after push", func() bool {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/apps/myapp", nil)
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
}

// readAdminAPIKey reads the super-admin API key the daemon writes to its
// data dir on first boot (mode 0600), the credential the CLI would be
// given with `cubeship login`.
func readAdminAPIKey(t *testing.T, dataDir string) string {
	t.Helper()
	var key string
	waitFor(t, 10*time.Second, "super-admin API key file", func() bool {
		data, err := os.ReadFile(filepath.Join(dataDir, "admin-api-key"))
		if err != nil {
			return false
		}
		key = strings.TrimSpace(string(data))
		return key != ""
	})
	return key
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
