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

func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Handle registers a route that is part of the documented API, using a
// Go 1.22 method-and-path pattern, e.g. "GET /apps/{name}". It must have
// a matching OpenAPI operation.
func (r *Router) Handle(pattern string, h http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.mux.Handle(pattern, h)
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
	r.mux.Handle(pattern, h)
}

func (r *Router) HandleInternalFunc(pattern string, h http.HandlerFunc) {
	r.HandleInternal(pattern, h)
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
