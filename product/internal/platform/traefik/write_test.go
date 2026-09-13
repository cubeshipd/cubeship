package traefik_test

import (
	"os"
	"path/filepath"
	"testing"

	"cubeship/internal/platform/traefik"
)

func routesPath(dir string) string {
	return filepath.Join(dir, "traefik-dynamic", traefik.RoutesFileName)
}

// Traefik refuses a document whose `http` has nothing under it, and
// refuses it as a failure to build the configuration *at all* — which
// takes the whole file provider down and the daemon's own router with
// it. So a machine with nothing to balance has no file, not an empty
// one.
func TestAMachineWithNothingToBalanceWritesNoFile(t *testing.T) {
	dir := t.TempDir()

	if changed, err := traefik.WriteRoutes(dir, nil, true); err != nil || changed {
		t.Fatalf("writing no routes: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(routesPath(dir)); !os.IsNotExist(err) {
		t.Errorf("an empty answer left a file behind: %v", err)
	}

	// And a machine that had routes and stops having them gets rid of
	// the one it wrote, rather than leaving routers pointing at
	// replicas that are not there any more.
	routes := []traefik.Route{{App: "web/production/api", Host: "api.example.com", Servers: []string{"http://one:8080"}}}
	if changed, err := traefik.WriteRoutes(dir, routes, true); err != nil || !changed {
		t.Fatalf("writing a route: changed=%v err=%v", changed, err)
	}
	if changed, err := traefik.WriteRoutes(dir, nil, true); err != nil || !changed {
		t.Fatalf("dropping the last route: changed=%v err=%v", changed, err)
	}
	if _, err := os.Stat(routesPath(dir)); !os.IsNotExist(err) {
		t.Errorf("the file survived the last route going: %v", err)
	}
}

// Traefik reloads on every write and this runs on a timer, so an answer
// that has not changed must not be written again — otherwise the proxy
// reloads every ten seconds for the life of the instance.
func TestTheSameAnswerIsNotWrittenTwice(t *testing.T) {
	dir := t.TempDir()
	routes := []traefik.Route{
		{App: "web/production/api", Host: "api.example.com", Servers: []string{"http://one:8080", "http://two:8080"}},
	}

	if changed, err := traefik.WriteRoutes(dir, routes, true); err != nil || !changed {
		t.Fatalf("the first write: changed=%v err=%v", changed, err)
	}
	if changed, err := traefik.WriteRoutes(dir, routes, true); err != nil || changed {
		t.Errorf("the same answer was written again: changed=%v err=%v", changed, err)
	}
	// A replica leaving is a change, and it is the whole of how one
	// leaves a load balancer.
	fewer := []traefik.Route{{App: routes[0].App, Host: routes[0].Host, Servers: []string{"http://one:8080"}}}
	if changed, err := traefik.WriteRoutes(dir, fewer, true); err != nil || !changed {
		t.Errorf("a replica leaving was not written: changed=%v err=%v", changed, err)
	}
}
