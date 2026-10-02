//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/platform/dockerx"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// This exercises the real Docker network boundary and production probe binary.
// It does not run the installer, updater, Traefik, or deployment orchestrator.
// A dedicated Engine is required because the application bridge has a fixed name.
func TestProbeNetworkTopology(t *testing.T) {
	if os.Getenv("CUBESHIP_PROBE_NETWORK_TEST") != "1" {
		t.Skip("set CUBESHIP_PROBE_NETWORK_TEST=1 on a dedicated Docker Engine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	command := func(name string, args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// Never borrow an operator's application bridge or remove their resources.
	if err := exec.CommandContext(ctx, "docker", "network", "inspect", dockerx.ApplicationNetwork).Run(); err == nil {
		t.Fatal("dedicated Engine required: cubeship network already exists")
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	privateNet, server, daemon, image := "cs-probe-private-"+suffix, "cs-probe-app-"+suffix, "cs-probe-daemon-"+suffix, "cs-probe-test:"+suffix
	dir := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	arch := command("docker", "version", "--format", "{{.Server.Arch}}")
	for _, build := range []struct {
		target string
		args   []string
	}{
		{"cubeshipd", []string{"build", "-o", filepath.Join(dir, "cubeshipd"), "./cmd/cubeshipd"}},
		{"probe-tests", []string{"test", "-c", "-tags=integration", "-o", filepath.Join(dir, "probe-tests"), "./test/integration"}},
	} {
		cmd := exec.CommandContext(ctx, "go", build.args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", build.target, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY cubeshipd /usr/local/bin/cubeshipd\nCOPY probe-tests /probe-tests\nENTRYPOINT [\"/probe-tests\",\"-test.run=^TestProbeNetworkFixture$\",\"-test.v\"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command("docker", "build", "-t", image, dir)
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", image).Run() })
	for _, n := range []string{dockerx.ApplicationNetwork, privateNet} {
		command("docker", "network", "create", n)
		t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", n).Run() })
	}
	for _, n := range []string{server, daemon} {
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", n).Run() })
	}
	command("docker", "run", "-d", "--name", server, "--network", dockerx.ApplicationNetwork, "-e", "CS_PROBE_FIXTURE=server", image)
	// Fresh installation: the daemon has only a management bridge.
	t.Log(command("docker", "run", "--name", daemon, "--network", privateNet, "-v", "/var/run/docker.sock:/var/run/docker.sock", "-e", "CS_PROBE_FIXTURE=driver", "-e", "CS_PROBE_SELF="+daemon, "-e", "CS_PROBE_APP="+server, "-e", "CS_PROBE_PRIVATE="+privateNet, image))
	t.Log("fresh and reconciled upgrade topology, HTTP/TCP success/failure, delayed readiness, cancellation and cleanup passed on", runtime.GOOS)
}

// Re-executed only in isolated fixture containers by TestProbeNetworkTopology.
func TestProbeNetworkFixture(t *testing.T) {
	mode := os.Getenv("CS_PROBE_FIXTURE")
	if mode == "" {
		t.Skip("container fixture")
	}
	if mode == "server" {
		var ready time.Time
		var readyMu sync.Mutex
		mux := http.NewServeMux()
		mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
		mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
		mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		mux.HandleFunc("/delayed", func(w http.ResponseWriter, r *http.Request) {
			readyMu.Lock()
			defer readyMu.Unlock()
			if ready.IsZero() {
				ready = time.Now().Add(2 * time.Second)
			}
			if time.Now().Before(ready) {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(200)
		})
		mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", 302) })
		go func() { _ = http.ListenAndServe("127.0.0.1:8081", mux) }()
		t.Fatal(http.ListenAndServe(":8080", mux))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	d, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	self, app, private := os.Getenv("CS_PROBE_SELF"), os.Getenv("CS_PROBE_APP"), os.Getenv("CS_PROBE_PRIVATE")
	if err := d.ConfigureProbes(ctx, self); err != nil {
		t.Fatal(err)
	}
	inspect, err := api.ContainerInspect(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	ip := inspect.NetworkSettings.Networks[dockerx.ApplicationNetwork].IPAddress
	app = inspect.ID
	checkIsolation := func() {
		t.Helper()
		info, err := api.ContainerInspect(ctx, self)
		if err != nil {
			t.Fatal(err)
		}
		if len(info.NetworkSettings.Networks) != 1 || info.NetworkSettings.Networks[private] == nil {
			t.Fatalf("daemon networks: %v", info.NetworkSettings.Networks)
		}
		direct, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		if err := dockerx.RunProbe(direct, "http", ip+":8080", "/ok"); err == nil {
			t.Fatal("management daemon unexpectedly reached app directly")
		}
	}
	checkCleanup := func() {
		t.Helper()
		items, err := api.ContainerList(ctx, container.ListOptions{All: true, Filters: filters.NewArgs(filters.Arg("label", "cubeship.health-probe=true"))})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Fatalf("leaked probe helpers: %v", items)
		}
	}
	checkIsolation()
	if err := d.ProbeHTTP(ctx, app, "/ok", 8080); err != nil {
		t.Fatalf("fresh topology HTTP probe: %v", err)
	}
	if err := d.ProbeTCP(ctx, app, 8080); err != nil {
		t.Fatalf("fresh topology TCP probe: %v", err)
	}
	checkCleanup()
	// An upgrade starts with the daemon on the old app bridge, then performs the
	// production network reconciliation before probing from management only.
	if err := d.ReconcileNetworks(ctx, self, []string{dockerx.ApplicationNetwork}, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileNetworks(ctx, self, []string{private}, []string{dockerx.ApplicationNetwork}); err != nil {
		t.Fatal(err)
	}
	checkIsolation()
	for _, tc := range []struct {
		name, path  string
		port        int
		tcp, wantOK bool
	}{
		{"HTTP ready", "/ok", 8080, false, true}, {"TCP ready", "", 8080, true, true},
		{"HTTP failure", "/fail", 8080, false, false}, {"TCP closed", "", 8099, true, false},
		{"redirect rejected", "/redirect", 8080, false, false}, {"loopback rejected", "/ok", 8081, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.tcp {
				err = d.ProbeTCP(ctx, app, tc.port)
			} else {
				err = d.ProbeHTTP(ctx, app, tc.path, tc.port)
			}
			if (err == nil) != tc.wantOK {
				t.Fatalf("probe: %v; want success %v", err, tc.wantOK)
			}
			checkCleanup()
		})
	}
	// Readiness eventually succeeds without changing the bridge membership.
	delayedFailed := false
	for attempt := 0; ; attempt++ {
		err := d.ProbeHTTP(ctx, app, "/delayed", 8080)
		if err == nil {
			if !delayedFailed {
				t.Fatal("delayed readiness never failed")
			}
			break
		}
		delayedFailed = true
		if attempt == 10 {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Run("cancellation and helper restrictions", func(t *testing.T) {
		probeCtx, stop := context.WithCancel(ctx)
		defer stop()
		done := make(chan error, 1)
		go func() { done <- d.ProbeHTTP(probeCtx, app, "/slow", 8080) }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			helpers, err := api.ContainerList(ctx, container.ListOptions{Filters: filters.NewArgs(filters.Arg("label", "cubeship.health-probe=true"))})
			if err != nil {
				t.Fatal(err)
			}
			if len(helpers) > 0 {
				h, err := api.ContainerInspect(ctx, helpers[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				if h.Config.User != "65534:65534" || !h.HostConfig.ReadonlyRootfs || len(h.Mounts) != 0 || len(h.HostConfig.PortBindings) != 0 || string(h.HostConfig.NetworkMode) != "container:"+app || h.HostConfig.RestartPolicy.Name != "no" || len(h.HostConfig.CapDrop) != 1 || h.HostConfig.CapDrop[0] != "ALL" || len(h.HostConfig.SecurityOpt) != 1 || h.HostConfig.SecurityOpt[0] != "no-new-privileges:true" {
					t.Fatalf("unexpected helper restrictions: %+v", h.HostConfig)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("helper did not start")
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("canceled probe succeeded")
			}
		case <-time.After(6 * time.Second):
			t.Fatal("cancellation did not return")
		}
		checkCleanup()
	})
	checkIsolation()
	checkCleanup()
}
