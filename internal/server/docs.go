package server

import (
	"net/http"
	"strings"
)

// scalarVersion pins the Scalar API reference build the docs page loads.
// Pinned rather than floating on @latest: an unpinned CDN script means
// the daemon's docs page can change — or break — without anything in this
// repository changing.
const scalarVersion = "1.25.61"

// docsHTML is the API reference page. Scalar renders it in the browser
// from the document at OpenAPIPath, so the daemon ships one small HTML
// file rather than a bundled UI.
//
// The script comes from a CDN, which means the reference page — and only
// that page — needs internet access to render. The document itself at
// OpenAPIPath is plain JSON and always works offline.
var docsHTML = strings.NewReplacer(
	"{{openapi}}", OpenAPIPath,
	"{{version}}", scalarVersion).Replace(`<!doctype html>
<html>
  <head>
    <title>Cubeship API</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>body { margin: 0 }</style>
  </head>
  <body>
    <script id="api-reference" data-url="{{openapi}}"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@{{version}}/dist/browser/standalone.min.js"></script>
  </body>
</html>
`)

// handleDocs serves the API reference.
//
// Like OpenAPIPath it is unauthenticated, because Scalar fetches the
// document from the browser with no credentials to offer. Neither
// endpoint exposes data — only the shape of the API — but an operator who
// would rather not advertise what runs here can block both at the proxy.
func (s *Server) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Scalar is the only third-party code this page may load, and it may
	// not talk to anywhere else.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' https://cdn.jsdelivr.net 'unsafe-inline'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write([]byte(docsHTML))
}
