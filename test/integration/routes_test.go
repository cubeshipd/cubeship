//go:build integration

// What Cubeship writes for Traefik, read by Traefik.
//
// Everything else about the routes file is checked by rendering it and
// looking at the string, which proves that we wrote what we meant and
// nothing about whether Traefik accepts it. Those are not the same
// question, and the difference has already cost one outage: a document
// with `http` and nothing under it renders fine, parses as YAML fine,
// and is refused by Traefik as a failure to build the configuration
// *at all* — taking the whole file provider down and the daemon's own
// router with it.
//
// So this test hands the real proxy the real file. It needs no daemon,
// no database and no registry: a throwaway Traefik with the file
// provider pointed at a directory, and its own log as the verdict.

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/bootstrap"
)

// traefikRejects hands Traefik a routes file and returns whatever it
// said about building its configuration.
//
// The backends do not exist and do not need to: what is under test is
// the parse, and a router whose servers are unreachable is a working
// router that answers 502. Traefik reports a *configuration* failure
// separately, and that is the string this looks for.
func traefikRejects(t *testing.T, routes []node.Route, tls bool) string {
	t.Helper()

	dir := t.TempDir()
	if _, err := node.WriteRoutes(dir, routes, tls); err != nil {
		t.Fatalf("write the routes file: %v", err)
	}
	// The daemon writes 0600 as root; this container runs as its own
	// user and has to be able to read it.
	_ = os.Chmod(filepath.Join(dir, "traefik-dynamic"), 0o755)
	if path := filepath.Join(dir, "traefik-dynamic", node.RoutesFileName); fileExists(path) {
		_ = os.Chmod(path, 0o644)
	}

	cmd := exec.Command("docker", "run", "--rm",
		"-v", filepath.Join(dir, "traefik-dynamic")+":/etc/traefik/dynamic:ro",
		bootstrap.TraefikImage,
		"--providers.file.directory=/etc/traefik/dynamic",
		"--providers.file.watch=true",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--log.level=ERROR")

	out, err := runFor(cmd, 8*time.Second)
	if err != nil && !strings.Contains(err.Error(), "signal") {
		t.Fatalf("run traefik: %v\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		// The one Traefik logs when a provider's configuration cannot
		// be assembled. It is the failure that is silent from outside:
		// containers keep running and every label-routed name keeps
		// working, so nothing looks broken but the file provider.
		if strings.Contains(line, "Error while building configuration") ||
			strings.Contains(line, "error while parsing") {
			return line
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// runFor runs a command, kills it after d, and returns what it printed.
func runFor(cmd *exec.Cmd, d time.Duration) (string, error) {
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-time.After(d):
		_ = cmd.Process.Kill()
		<-done
		return buf.String(), nil
	}
}

// The load balancer's own file: several backends, a retry over them and
// a health check on the service.
func TestTraefikAcceptsABalancedRoute(t *testing.T) {
	if said := traefikRejects(t, []node.Route{{
		App:     "web/production/api",
		Host:    "api.example.com",
		Servers: []string{"http://one:8080", "http://two:8080"},
		Health:  "/healthz",
	}}, false); said != "" {
		t.Errorf("Traefik refused what this instance writes for a balanced app:\n%s", said)
	}
}

// And the same without a health path, which is what every app starts
// as: a retry and no check.
func TestTraefikAcceptsABalancedRouteWithNoHealthCheck(t *testing.T) {
	if said := traefikRejects(t, []node.Route{{
		App:     "web/production/api",
		Host:    "api.example.com",
		Servers: []string{"http://one:8080", "http://two:8080"},
	}}, false); said != "" {
		t.Errorf("Traefik refused a balanced app with no health check:\n%s", said)
	}
}

// The case that already broke: nothing to balance. It has to leave no
// file rather than an empty document, so what Traefik reads here is a
// directory with nothing in it — which it must accept in silence.
func TestTraefikAcceptsAMachineWithNothingToBalance(t *testing.T) {
	if said := traefikRejects(t, nil, false); said != "" {
		t.Errorf("Traefik refused a machine with nothing to balance:\n%s", said)
	}
}
