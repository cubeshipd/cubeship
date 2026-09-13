package httpx

import (
	"net/http"
	"slices"
	"strings"
)

// Router is an http.ServeMux that remembers what was registered on it,
// and whether each route belongs to the documented API.
//
// The recorded patterns are what lets the OpenAPI document be checked
// against reality: a documented route with no operation, or an operation
// describing a route that doesn't exist, both fail a test rather than
// quietly shipping a spec that lies.
//
// Not every route belongs in the document — the daemon also serves
// machinery that no API consumer calls. Those are registered through
// HandleInternal, so leaving one out of the document is a decision
// someone wrote down rather than something they forgot.
type Router struct {
	mux      *http.ServeMux
	patterns []string
	internal []string
}

// APIPrefix is where everything a client calls lives, so the root is
// free for the dashboard.
//
// Without it the two collide immediately: GET /setup is the API's "does
// this instance need setting up", and it is also the page that answers
// it. A dashboard that cannot name its pages after the resources they
// show is broken by construction, and moving the API is the change that
// only has to happen once.
//
// Patterns are recorded WITHOUT the prefix. It is one constant applied
// in one place, so the OpenAPI document keeps describing /orgs and says
// where /orgs is by ending its server URL in the prefix.
const APIPrefix = "/api"

func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Handle registers a route that is part of the documented API, using a
// Go 1.22 method-and-path pattern, e.g. "GET /apps/{name}". It must have
// a matching OpenAPI operation.
func (r *Router) Handle(pattern string, h http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.mux.Handle(prefixed(pattern), h)
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.Handle(pattern, h)
}

// HandleInternal registers a route that is deliberately absent from the
// OpenAPI document: infrastructure the daemon serves to itself or to
// Docker, and self-service plumbing that only the CLI drives. It must
// NOT have an OpenAPI operation.
func (r *Router) HandleInternal(pattern string, h http.Handler) {
	r.internal = append(r.internal, pattern)
	r.mux.Handle(prefixed(pattern), h)
}

func (r *Router) HandleInternalFunc(pattern string, h http.HandlerFunc) {
	r.HandleInternal(pattern, h)
}

// HandleRoot registers a route at the address it names, outside the API
// prefix. It is for what is not the API: the dashboard, the liveness
// probe, the OpenAPI document and the page that renders it, the MCP
// endpoint, and the two URLs the registry container is configured with.
// Those addresses are either typed by a person or written into another
// program's configuration, so they do not move.
//
// Nothing registered here is documented.
func (r *Router) HandleRoot(pattern string, h http.Handler) {
	r.internal = append(r.internal, pattern)
	r.mux.Handle(pattern, h)
}

func (r *Router) HandleRootFunc(pattern string, h http.HandlerFunc) {
	r.HandleRoot(pattern, h)
}

// prefixed moves a pattern under the API prefix, keeping its method.
func prefixed(pattern string) string {
	method, path := SplitPattern(pattern)
	if method == "" {
		return APIPrefix + path
	}
	return method + " " + APIPrefix + path
}

// Patterns returns every documented route pattern, sorted.
func (r *Router) Patterns() []string {
	out := slices.Clone(r.patterns)
	slices.Sort(out)
	return out
}

// InternalPatterns returns every route deliberately kept out of the
// document, sorted.
func (r *Router) InternalPatterns() []string {
	out := slices.Clone(r.internal)
	slices.Sort(out)
	return out
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// SplitPattern breaks "GET /apps/{name}" into its method and path. A
// pattern with no method (one that matches every verb) yields an empty
// method.
func SplitPattern(pattern string) (method, path string) {
	if i := strings.IndexByte(pattern, ' '); i >= 0 {
		return pattern[:i], strings.TrimSpace(pattern[i+1:])
	}
	return "", pattern
}
