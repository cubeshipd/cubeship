package node

import (
	"bytes"
	"os"
	"path/filepath"

	"cubeship/internal/platform/traefik"
)

// RoutesFileName is what the file is called in Traefik's dynamic
// directory, on every machine that has one.
const RoutesFileName = "apps.yml"

// WriteRoutes writes a machine's routes where its Traefik is watching,
// and reports whether anything changed.
//
// Written only when it changed, because Traefik reloads on every write
// and this runs on a timer: a file rewritten identically every ten
// seconds is a proxy reloading every ten seconds. The rendering is
// sorted for the same reason — a map's iteration order would make every
// pass look like a change.
//
// One function, called by the control plane for itself and by the agent
// for the machine it is on, so the two cannot drift into writing
// different files from the same answer.
func WriteRoutes(dataDir string, routes []Route, tls bool) (bool, error) {
	dir := filepath.Join(dataDir, "traefik-dynamic")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, RoutesFileName)

	// **Nothing to serve is no file**, not an empty document.
	//
	// Traefik refuses a file whose `http` has nothing under it — "http
	// cannot be a standalone element" — and it refuses it as a failure
	// to build the configuration *at all*, which takes the whole file
	// provider down with it. That is not this file going quiet: it is
	// the daemon's own router disappearing too, so the instance stops
	// answering at its own name while every container Traefik
	// discovered goes on working. Exactly the shape of failure the
	// pinned Traefik version exists to avoid.
	//
	// Removing it is safe in the way an empty document was meant to be:
	// the provider watches the directory, so a file that goes takes its
	// routers with it and nothing else.
	if len(routes) == 0 {
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}

	next := []byte(traefik.RoutesYAML(routesToTraefik(routes), tls))
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, next) {
		return false, nil
	}
	return true, os.WriteFile(path, next, 0o600)
}

func routesToTraefik(routes []Route) []traefik.Route {
	out := make([]traefik.Route, 0, len(routes))
	for _, r := range routes {
		out = append(out, traefik.Route{App: r.App, Host: r.Host, Servers: r.Servers, Health: r.Health})
	}
	return out
}
