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
	next := []byte(traefik.RoutesYAML(routesToTraefik(routes), tls))
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, next) {
		return false, nil
	}
	return true, os.WriteFile(path, next, 0o600)
}

func routesToTraefik(routes []Route) []traefik.Route {
	out := make([]traefik.Route, 0, len(routes))
	for _, r := range routes {
		out = append(out, traefik.Route{App: r.App, Host: r.Host, Servers: r.Servers})
	}
	return out
}
