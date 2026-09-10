package traefik_test

import (
	"strings"
	"testing"

	"cubeship/internal/platform/traefik"
)

// The file is written on a timer and rewritten only when it changed, so
// the same answer has to render to the same bytes. A map's iteration
// order would make every pass look like a change and reload Traefik
// every ten seconds, for the life of the instance.
func TestTheSameRoutesRenderTheSameWayEveryTime(t *testing.T) {
	routes := []traefik.Route{
		{App: "web/production/api", Host: "b.example.com", Servers: []string{"http://two:8080"}},
		{App: "web/production/api", Host: "a.example.com", Servers: []string{"http://one:8080"}},
		{App: "web/production/site", Host: "c.example.com", Servers: []string{"http://three:8080"}},
	}
	first := traefik.RoutesYAML(routes, true)
	shuffled := []traefik.Route{routes[2], routes[0], routes[1]}
	if second := traefik.RoutesYAML(shuffled, true); first != second {
		t.Errorf("the same routes rendered two ways:\n%s\n---\n%s", first, second)
	}
}

// A container keeps the labels it was created with, so an app that has
// just gained a second machine still has a label-router for its name on
// its edge until it is redeployed. Two routers for one host is a race
// nobody can see the result of; the priority is what makes the balancer
// the answer immediately.
func TestTheBalancerOutranksAStaleLabelRouter(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{
		{App: "web/production/api", Host: "api.example.com", Servers: []string{"http://one:8080", "http://two:8080"}},
	}, true)

	if !strings.Contains(out, "priority: 10000") {
		t.Errorf("no priority, so a stale label may still win:\n%s", out)
	}
	if !strings.Contains(out, "certResolver: letsencrypt") || !strings.Contains(out, "- websecure") {
		t.Errorf("the router does not terminate TLS, on the one machine that can:\n%s", out)
	}
	for _, want := range []string{"http://one:8080", "http://two:8080"} {
		if !strings.Contains(out, want) {
			t.Errorf("%s is not a backend:\n%s", want, out)
		}
	}
}

// Nothing to serve renders nothing, and the caller writes no file.
//
// A document with `http` and nothing under it is refused by Traefik —
// and refused as a failure to build the configuration at all, which
// takes the whole file provider down and the daemon's own router with
// it. An instance then stops answering at its own name while every
// container Traefik discovered goes on working, which reads as anything
// but a proxy that cannot see.
func TestNothingToServeRendersNothing(t *testing.T) {
	if out := traefik.RoutesYAML(nil, true); out != "" {
		t.Errorf("an empty answer rendered %q, and Traefik refuses a standalone http key", out)
	}
}

// Without a domain there are no certificates, so the routers sit on the
// plain entrypoint. Pointing one at :443 with no resolver would make
// every name it serves unreachable rather than merely unencrypted —
// which is the same rule Labels follows.
func TestWithNoCertificatesTheRoutersStayOnPlainHTTP(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{
		{App: "web/production/api", Host: "api.example.com", Servers: []string{"http://one:8080"}},
	}, false)
	if strings.Contains(out, "certResolver") || strings.Contains(out, "websecure") {
		t.Errorf("an instance with no certificates asked for one:\n%s", out)
	}
	if !strings.Contains(out, "- web") {
		t.Errorf("the router is on no entrypoint at all:\n%s", out)
	}
}

// **A dead replica costs a retry rather than a 502.** This is the half
// of the health story that needs nothing configured: a container that
// has gone refuses the connection, and without a retry that refusal is
// what the visitor gets — one request in three failing on an app with
// three replicas, for as long as it takes the machine that lost it to
// say so.
func TestADeadReplicaIsRetriedAgainstTheNextOne(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{{
		App: "web/production/api", Host: "api.example.com",
		Servers: []string{"http://one:8080", "http://two:8080", "http://three:8080"},
	}}, true)

	if !strings.Contains(out, "retry:") || !strings.Contains(out, "attempts: 3") {
		t.Errorf("no retry, so a gone replica answers 502 to its share of the traffic:\n%s", out)
	}
	if !strings.Contains(out, "middlewares:") {
		t.Errorf("the retry is defined and never attached to the router:\n%s", out)
	}
}

// One backend is nothing to retry against: attempts count the first
// try, so a retry there is the same dead container asked twice — a
// visitor waiting twice as long for the same 502.
func TestOneBackendIsNotRetried(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{{
		App: "web/production/api", Host: "api.example.com",
		Servers: []string{"http://one:8080"},
	}}, true)

	if strings.Contains(out, "retry:") {
		t.Errorf("a single backend was given a retry against itself:\n%s", out)
	}
}

// The other half, and the one that needs a path: a replica that is **up
// and broken** answers the connection, so no retry ever fires for it.
// A check is what takes that one out of rotation before a visitor
// reaches it rather than after.
func TestAHealthPathTakesABrokenReplicaOutOfRotation(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{{
		App: "web/production/api", Host: "api.example.com",
		Servers: []string{"http://one:8080", "http://two:8080"},
		Health:  "/healthz",
	}}, true)

	if !strings.Contains(out, "healthCheck:") || !strings.Contains(out, `path: "/healthz"`) {
		t.Errorf("the path did not reach the load balancer:\n%s", out)
	}
	// The cadence is the cluster's own, and the timeout is deliberately
	// not tight: marking a working container down is worse than leaving
	// a dead one in for one more interval.
	if !strings.Contains(out, "interval: 10s") || !strings.Contains(out, "timeout: 5s") {
		t.Errorf("the check has no bounds on it:\n%s", out)
	}
}

// No path is no check, which is what every app starts as. A default
// path would be a guess, and a wrong guess here does not degrade a name
// — it takes every replica out at once and answers 503.
func TestNoHealthPathIsNoCheck(t *testing.T) {
	out := traefik.RoutesYAML([]traefik.Route{{
		App: "web/production/api", Host: "api.example.com",
		Servers: []string{"http://one:8080", "http://two:8080"},
	}}, true)

	if strings.Contains(out, "healthCheck") {
		t.Errorf("an app that asked for no check got one:\n%s", out)
	}
}
