//go:build integration

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cubeship/internal/platform/dockerx"
)

// Exercise real bridge forwarding, not just Docker DNS or rendered options.
func TestManagementMigrationBlocksApplicationTraffic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	waitHTTP := func(container, host string) {
		t.Helper()
		var last []byte
		for attempt := 0; attempt < 20; attempt++ {
			out, err := exec.CommandContext(ctx, "docker", "exec", container, "wget", "-qO-", "-T", "2", "http://"+host+":8080").CombinedOutput()
			if err == nil {
				return
			}
			last = out
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("service did not become reachable: %s", last)
	}
	suffix := fmt.Sprint(time.Now().UnixNano())
	appNet, privateNet := "cs-app-"+suffix, "cs-private-"+suffix
	for _, name := range []string{appNet, privateNet} {
		docker("network", "create", name)
		t.Cleanup(func() { _ = exec.Command("docker", "network", "rm", name).Run() })
	}
	server, probe := "cs-server-"+suffix, "cs-probe-"+suffix
	for _, name := range []string{server, probe} {
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	}
	docker("run", "-d", "--name", server, "--network", appNet, "alpine:3.23", "sh", "-c", "mkdir -p /www; echo private > /www/index.html; exec httpd -f -p 8080 -h /www")
	docker("run", "-d", "--name", probe, "--network", appNet, "--cap-drop", "NET_RAW", "--security-opt", "no-new-privileges:true", "alpine:3.23", "sleep", "120")
	waitHTTP(probe, server)
	client, err := dockerx.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ReconcileNetworks(ctx, server, []string{privateNet}, []string{appNet}); err != nil {
		t.Fatal(err)
	}
	ip := docker("inspect", "--format", "{{(index .NetworkSettings.Networks \""+privateNet+"\").IPAddress}}", server)
	for _, host := range []string{server, ip} {
		if out, err := exec.CommandContext(ctx, "docker", "exec", probe, "wget", "-qO-", "-T", "2", "http://"+host+":8080").CombinedOutput(); err == nil {
			t.Fatalf("application reached private service via %s: %s", host, out)
		}
	}
	// A trusted container on both bridges can still reach the service.
	if err := client.ReconcileNetworks(ctx, probe, []string{privateNet}, nil); err != nil {
		t.Fatal(err)
	}
	waitHTTP(probe, server)
	docker("restart", server)
	waitHTTP(probe, server)
	if err := client.ReconcileNetworks(ctx, probe, nil, []string{privateNet}); err != nil {
		t.Fatal(err)
	}
	ip = docker("inspect", "--format", "{{(index .NetworkSettings.Networks \""+privateNet+"\").IPAddress}}", server)
	if out, err := exec.CommandContext(ctx, "docker", "exec", probe, "wget", "-qO-", "-T", "2", "http://"+ip+":8080").CombinedOutput(); err == nil {
		t.Fatalf("restart exposed private service: %s", out)
	}

}
