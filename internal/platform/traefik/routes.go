package traefik

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A router this machine serves that no container of its own carries the
// labels for.
//
// Traefik's Docker provider only ever sees the Engine it is pointed at,
// so a machine can discover the containers on itself and none of the
// ones on the rest of the cluster. What crosses that gap is this: the
// control plane knows where every replica of an app is, and writes the
// backends out as a file the machine's Traefik reads. The addresses are
// container names, which resolve on the mesh.
type Route struct {
	// App is the app's reference, used to name the router. One router
	// and one service per (app, host), the same rule Labels follows and
	// for the same reason: a host carries its own port.
	App  string
	Host string
	// Servers are where the traffic goes, in order — one per replica.
	// Traefik round-robins between them, which is the whole of the load
	// balancing: what makes it work is that every one of these names
	// resolves from this machine, over the cluster's overlay.
	Servers []string
	// Health is the path Traefik asks each replica for to decide
	// whether it is worth sending traffic to. Empty is no check, which
	// is the default: a path is a thing only the app's author knows,
	// and a wrong one takes every replica out of rotation at once.
	Health string
}

// How hard the edge looks at a replica before it stops trusting it.
//
// The interval is the cluster's own, so a dead replica leaves the
// balancer on the same cadence everything else here moves at — and
// checking faster would be a request per replica per second for a
// container that is almost always fine.
//
// The timeout is deliberately not tight. A replica that takes five
// seconds to answer is in trouble, and one taking one second under load
// is not — marking a working container down is a worse outcome than
// leaving a dead one in for one more interval, because it is the
// failure that takes a name off the internet rather than degrading it.
const (
	HealthInterval = 10 * time.Second
	HealthTimeout  = 5 * time.Second
)

// ServerURL is one replica as a backend address.
func ServerURL(container string, port int) string {
	return "http://" + container + ":" + strconv.Itoa(port)
}

// RoutesPriority is what the file's routers are given, and it is
// deliberately larger than any rule Traefik would score for itself.
//
// A container keeps the labels it was created with, so an app that has
// just gained a second machine still has a label-router for its name on
// the edge until it is redeployed. Two routers for one host is a race
// nobody can see the result of; a priority makes the balancer the
// answer immediately, and the stale one falls away with the container.
const RoutesPriority = 10000

// RoutesYAML renders a machine's routes as a Traefik file-provider
// document.
//
// Sorted, and that is not cosmetic: the file is written on a timer and
// rewritten only when it changed, so a map's iteration order would make
// every pass look like a change and reload Traefik every ten seconds.
func RoutesYAML(routes []Route, tls bool) string {
	if len(routes) == 0 {
		// Nothing at all, and the caller writes no file rather than
		// this one. Traefik refuses a document whose `http` has nothing
		// under it, and refuses it as a failure to build the
		// configuration at all — which takes the whole file provider
		// down, the daemon's own router with it. See node.WriteRoutes.
		return ""
	}
	sorted := append([]Route(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].App != sorted[j].App {
			return sorted[i].App < sorted[j].App
		}
		return sorted[i].Host < sorted[j].Host
	})

	entrypoint := "web"
	if tls {
		entrypoint = "websecure"
	}

	var routers, services, middlewares strings.Builder
	for _, r := range sorted {
		name := routerName(r.App, r.Host)
		fmt.Fprintf(&routers, "    %s:\n", name)
		fmt.Fprintf(&routers, "      rule: \"Host(`%s`)\"\n", r.Host)
		fmt.Fprintf(&routers, "      priority: %d\n", RoutesPriority)
		fmt.Fprintf(&routers, "      entrypoints:\n        - %s\n", entrypoint)
		if tls {
			routers.WriteString("      tls:\n        certResolver: letsencrypt\n")
		}
		// **A dead replica costs a retry rather than a 502**, and this
		// is the half of the answer that needs nothing configured. A
		// container that has gone refuses the connection, and without
		// this that refusal is what the visitor gets: one request in
		// three failing on an app with three replicas, for as long as
		// it takes the machine that lost it to say so. With it, the
		// request goes to the next replica and nobody sees anything.
		//
		// Only with something to retry *against*. Attempts count the
		// first try, so a second server is two attempts; on one server
		// a retry is the same dead container asked twice.
		if len(r.Servers) > 1 {
			fmt.Fprintf(&routers, "      middlewares:\n        - %s-retry\n", name)
			fmt.Fprintf(&middlewares, "    %s-retry:\n      retry:\n        attempts: %d\n", name, len(r.Servers))
		}
		fmt.Fprintf(&routers, "      service: %s\n", name)

		fmt.Fprintf(&services, "    %s:\n      loadBalancer:\n        servers:\n", name)
		for _, s := range r.Servers {
			fmt.Fprintf(&services, "          - url: \"%s\"\n", s)
		}
		// And the other half, which does need a path: a replica that is
		// **up and broken** answers the connection, so no retry ever
		// fires for it. A check is what takes that one out of rotation
		// before a visitor reaches it, rather than after.
		if r.Health != "" {
			fmt.Fprintf(&services, "        healthCheck:\n          path: %q\n", r.Health)
			fmt.Fprintf(&services, "          interval: %s\n          timeout: %s\n",
				HealthInterval, HealthTimeout)
		}
	}
	out := "http:\n  routers:\n" + routers.String() + "  services:\n" + services.String()
	if middlewares.Len() > 0 {
		out += "  middlewares:\n" + middlewares.String()
	}
	return out
}

// routerName is a name unique per (app, host) and safe in YAML.
//
// The host is in it because an app answers at several names and each
// carries its own port, so two hosts of one app are two services.
func routerName(app, host string) string {
	return "cubeship-lb-" + safe(app) + "-" + safe(host)
}

func safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
