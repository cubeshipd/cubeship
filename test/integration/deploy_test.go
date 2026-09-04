//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
		return resp.StatusCode == http.StatusOK
	})

	ctx := context.Background()
	client := apiclient.New("http://127.0.0.1:9000", testToken)

	image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.localtest.me/myapp" {
		t.Fatalf("unexpected image: %q", image)
	}

	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/myapp:latest", "./testapp")
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}
	push := exec.Command("docker", "push", "localhost:5000/myapp:latest")
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push fixture image: %v\n%s", err, out)
	}

	waitFor(t, 60*time.Second, "app deployed after push", func() bool {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/apps/myapp", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
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

func jsonDecode(resp *http.Response, v any) error {
	return json.NewDecoder(resp.Body).Decode(v)
}

func jsonDecodeOrFatal(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := jsonDecode(resp, v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
