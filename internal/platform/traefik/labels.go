package traefik

import (
	"fmt"
	"strconv"
)

// Labels returns the Docker labels that make Traefik route a domain to a
// container's port.
//
// tls says whether the instance can issue certificates, which it can only
// once a contact address is configured. Without it the router sits on the
// plain-HTTP entrypoint and asks for no certificate: pointing it at
// :443 with no resolver would make the app unreachable rather than
// merely unencrypted.
//
// A container keeps the labels it was created with, so an app deployed
// before certificates were possible stays on HTTP until it is redeployed.
func Labels(routerName, domain string, port int, tls bool) map[string]string {
	router := "cubeship-" + routerName
	if routerName == "" {
		router = "cubeship"
	}

	entrypoint := "web"
	if tls {
		entrypoint = "websecure"
	}

	labels := map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers." + router + ".rule":                      fmt.Sprintf("Host(`%s`)", domain),
		"traefik.http.routers." + router + ".entrypoints":               entrypoint,
		"traefik.http.services." + router + ".loadbalancer.server.port": strconv.Itoa(port),
		"traefik.docker.network":                                        "cubeship",
	}
	if tls {
		labels["traefik.http.routers."+router+".tls.certresolver"] = "letsencrypt"
	}
	return labels
}
