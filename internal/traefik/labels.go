package traefik

import (
	"fmt"
	"strconv"
)

// Labels returns the Docker labels that make Traefik route the given
// domain to an app container's port, with automatic TLS.
func Labels(appName, domain string, port int) map[string]string {
	router := "cubeship-" + appName
	return map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers." + router + ".rule":                      fmt.Sprintf("Host(`%s`)", domain),
		"traefik.http.routers." + router + ".entrypoints":               "websecure",
		"traefik.http.routers." + router + ".tls.certresolver":          "letsencrypt",
		"traefik.http.services." + router + ".loadbalancer.server.port": strconv.Itoa(port),
		"traefik.docker.network":                                        "cubeship",
	}
}
