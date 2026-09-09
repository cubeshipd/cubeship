package traefik

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
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
}

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

	var routers, services strings.Builder
	for _, r := range sorted {
		name := routerName(r.App, r.Host)
		fmt.Fprintf(&routers, "    %s:\n", name)
		fmt.Fprintf(&routers, "      rule: \"Host(`%s`)\"\n", r.Host)
		fmt.Fprintf(&routers, "      priority: %d\n", RoutesPriority)
		fmt.Fprintf(&routers, "      entrypoints:\n        - %s\n", entrypoint)
		if tls {
			routers.WriteString("      tls:\n        certResolver: letsencrypt\n")
		}
		fmt.Fprintf(&routers, "      service: %s\n", name)

		fmt.Fprintf(&services, "    %s:\n      loadBalancer:\n        servers:\n", name)
		for _, s := range r.Servers {
			fmt.Fprintf(&services, "          - url: \"%s\"\n", s)
		}
	}
	return "http:\n  routers:\n" + routers.String() + "  services:\n" + services.String()
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
