package httpx

import (
	"net/http"
	"slices"
	"strings"
)

// Router is an http.ServeMux that remembers what was registered on it.
//
// The recorded patterns are what lets the OpenAPI document be checked
// against reality: a route with no documented operation, or an operation
// describing a route that doesn't exist, both fail a test rather than
// quietly shipping a spec that lies.
type Router struct {
	mux      *http.ServeMux
	patterns []string
}

func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// Handle registers a Go 1.22 method-and-path pattern, e.g.
// "GET /apps/{name}".
func (r *Router) Handle(pattern string, h http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.mux.Handle(pattern, h)
}

func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.Handle(pattern, h)
}

// Patterns returns every registered pattern, sorted.
func (r *Router) Patterns() []string {
	out := slices.Clone(r.patterns)
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
