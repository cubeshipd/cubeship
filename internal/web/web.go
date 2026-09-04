// Package web serves the dashboard: the built front-end, compiled into
// the daemon so a Cubeship install is still one binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist is the front-end build output. It is not in the repository —
// `make web` puts it here — so a checkout with no Node installed still
// compiles. The .gitkeep is what keeps this embed legal in that state,
// and Handler says so plainly rather than serving a blank page.
//
//go:embed all:dist
var dist embed.FS

// Handler serves the dashboard, with the fallback a single-page app
// needs: a path that names no file is a route the front-end router
// resolves itself, so it gets index.html and a 200. A path that looks
// like an asset gets a 404 instead — answering a missing script with
// HTML turns a broken build into a confusing parse error.
func Handler() http.Handler {
	files, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return HandlerFor(files)
}

// HandlerFor is Handler over any filesystem, so the routing above can be
// tested against a build without one having to exist.
func HandlerFor(files fs.FS) http.Handler {
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return http.HandlerFunc(unbuilt)
	}
	return &handler{files: files}
}

type handler struct {
	files fs.FS
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Mounted for every method, because the root pattern cannot be
	// method-scoped without becoming ambiguous with the API's. Static
	// files answer reads and nothing else.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

	// The exported front-end writes /login as login.html, so a route
	// resolves to a file before it falls back to the app shell.
	for _, candidate := range []string{name, name + ".html", path.Join(name, "index.html")} {
		if candidate == "" || candidate == ".html" {
			continue
		}
		if info, err := fs.Stat(h.files, candidate); err == nil && !info.IsDir() {
			// ServeFileFS rather than a FileServer: a FileServer
			// redirects /index.html to its directory, which would turn
			// every route into a 301 back to itself.
			h.setCacheHeaders(w, candidate)
			http.ServeFileFS(w, r, h.files, candidate)
			return
		}
	}

	if path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}

	h.setCacheHeaders(w, "index.html")
	http.ServeFileFS(w, r, h.files, "index.html")
}

// setCacheHeaders keeps the shell fresh and lets the fingerprinted
// bundles be cached forever. Getting this backwards is what makes a
// browser keep running the previous release after an upgrade.
func (h *handler) setCacheHeaders(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, "_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

// unbuilt answers when the binary was compiled without a dashboard. It
// says which command builds one, because the alternative — a 404 at the
// address the installer tells you to open — reads like a broken install.
func unbuilt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("This daemon was built without the dashboard. Run `make web` and rebuild.\n" +
		"The API is up, under /api — see /docs.\n"))
}
