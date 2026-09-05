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

// scalarIntegrity is the SHA-384 of exactly that build, and the browser
// refuses the script if what arrives does not hash to it.
//
// The version pin says which file to ask for; this says what the file
// is. They are not the same promise: a CDN that served something else
// under that version — compromised, or an account taken over — would run
// its code on the same origin as the session cookie, since /docs is this
// daemon's own address. Nothing on this page is worth stealing, but the
// cookie beside it is.
//
// To change it: fetch the file at the version above and take
//
//	openssl dgst -sha384 -binary <file> | openssl base64 -A
//
// A wrong value is not subtle — the page renders nothing at all.
const scalarIntegrity = "sha384-YS+JKn/OdeHGu8uLJCRXHeo0whpN5qJCQvOJz6QKJoEErOQ+0xgfWlBEgHOIRDvl"

// docsHTML is the API reference page. Scalar renders it in the browser
// from the document at OpenAPIPath, so the daemon ships one small HTML
// file rather than a bundled UI.
//
// The script comes from a CDN, which means the reference page — and only
// that page — needs internet access to render. The document itself at
// OpenAPIPath is plain JSON and always works offline.
var docsHTML = strings.NewReplacer(
	"{{openapi}}", OpenAPIPath,
	"{{version}}", scalarVersion,
	"{{integrity}}", scalarIntegrity).Replace(`<!doctype html>
<html>
  <head>
    <title>Cubeship API</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <style>body { margin: 0 }</style>
  </head>
  <body>
    <script id="api-reference" data-url="{{openapi}}"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@{{version}}/dist/browser/standalone.min.js" integrity="{{integrity}}" crossorigin="anonymous"></script>
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
	//
	// No 'unsafe-inline' for scripts: the page has no inline script of
	// its own — the one <script> element carrying the document's URL has
	// no body — and Scalar renders without one. Styles still need it,
	// because Scalar injects them.
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write([]byte(docsHTML))
}
