package traefik

import (
	"fmt"
	"strconv"
)

// Domain is one name an app answers at, and the port behind it.
//
// The pair is the unit because the question "which port?" has no single
// answer once an app has more than one name: api.example.com and
// admin.example.com on one image are two ports on one container.
type Domain struct {
	Host string
	Port int
}

// Labels returns the Docker labels that make Traefik route an app's
// domains to its container.
//
// **One router and one service per domain**, not one rule listing them
// all. A rule of `Host(a) || Host(b)` is a single router, and a router
// has one service — so every name behind it would reach one port. The
// names are what carry the ports, so they cannot share a router.
//
// tls says whether the instance can issue certificates, which it can
// only once a contact address is configured. Without it a router sits
// on the plain-HTTP entrypoint and asks for no certificate: pointing it
// at :443 with no resolver would make the app unreachable rather than
// merely unencrypted.
//
// A container keeps the labels it was created with, so an app deployed
// before certificates were possible stays on HTTP until it is
// redeployed — and an app that gained a domain is not serving it until
// then either.
func Labels(routerName string, domains []Domain, tls bool) map[string]string {
	base := "cubeship-" + routerName
	if routerName == "" {
		base = "cubeship"
	}

	entrypoint := "web"
	if tls {
		entrypoint = "websecure"
	}

	labels := map[string]string{"traefik.docker.network": "cubeship"}
	if len(domains) == 0 {
		// Nothing to route. `traefik.enable` is left off rather than set
		// false so the container is simply not something Traefik has an
		// opinion about.
		return labels
	}
	labels["traefik.enable"] = "true"

	for i, d := range domains {
		// The first router keeps the app's plain name, so an app with
		// one domain produces exactly the labels it always did and
		// existing containers are unchanged by this.
		router := base
		if i > 0 {
			router = fmt.Sprintf("%s-%d", base, i)
		}

		labels["traefik.http.routers."+router+".rule"] = fmt.Sprintf("Host(`%s`)", d.Host)
		labels["traefik.http.routers."+router+".entrypoints"] = entrypoint
		labels["traefik.http.routers."+router+".service"] = router
		labels["traefik.http.services."+router+".loadbalancer.server.port"] = strconv.Itoa(d.Port)
		if tls {
			labels["traefik.http.routers."+router+".tls.certresolver"] = "letsencrypt"
		}
	}
	return labels
}
