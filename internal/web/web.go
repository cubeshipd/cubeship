// Package web puts the dashboard in front of the API.
//
// The dashboard is a Next server in its own container, and the daemon
// is the only thing in front of it: it answers /api itself and hands
// everything else here, which proxies to that container.
//
// One address, therefore, and that is the point. Cubeship is installed
// with one command and reached by IP before there is a domain; a
// dashboard on a second port would be a second thing to open, a second
// thing to firewall, and a same-origin session cookie would stop being
// same-origin.
package web

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// Handler proxies to the dashboard's server at addr.
//
// The address is resolved on every request rather than at startup: the
// container may not exist when the daemon first serves, and a proxy
// that had cached a failed lookup would keep failing after it came up.
func Handler(addr string) http.Handler {
	target := &url.URL{Scheme: "http", Host: addr}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			// The dashboard renders links and redirects against the
			// name the browser used, not against the container's.
			r.Out.Host = r.In.Host
			r.SetXForwarded()
		},
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			// The dashboard streams its HTML. Buffering a whole page
			// before sending any of it would undo that for no gain.
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   16,
		},
		FlushInterval: -1,
		ErrorHandler:  unreachable,
	}
	return proxy
}

// unreachable answers when the dashboard's container is not up.
//
// It says which container and what to look at, because the alternative
// — a bare 502 at the address the installer tells you to open — reads
// like a broken install rather than a container that has not started
// yet. The API is unaffected and says so: someone whose dashboard is
// down can still reach /docs and the CLI still works.
func unreachable(w http.ResponseWriter, r *http.Request, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	w.Write([]byte(
		"The dashboard is not answering.\n\n" +
			"It runs as its own container, cubeship-frontend, which the daemon starts from " +
			"the image named in CUBESHIP_WEB_IMAGE.\n\n" +
			"Two places say why:\n" +
			"  docker logs cubeship-daemon    — if the container was never started\n" +
			"  docker logs cubeship-frontend  — if it started and then failed\n\n" +
			"The API is unaffected: it is under /api, and /docs describes it.\n"))
}
