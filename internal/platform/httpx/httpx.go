// Package httpx holds the small helpers every module's HTTP handlers
// share, so none of them hand-rolls JSON encoding or status codes.
package httpx

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// WriteJSON writes v as the response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ErrNotJSON is a request body sent as something other than JSON.
var ErrNotJSON = errors.New("body must be application/json")

// DecodeJSON reads a JSON request body into v.
//
// A body that is present must be declared as JSON. That is not
// pedantry: a form, or a fetch() with no headers set, sends
// application/x-www-form-urlencoded, multipart/form-data or text/plain,
// and those three are exactly the content types a browser will send
// cross-site *without* a preflight. Refusing them means every
// state-changing endpoint here needs a preflight the other origin cannot
// pass, on top of the same-origin check in the session middleware.
//
// An absent Content-Type with no body is left alone — that is the CLI
// asking for something with nothing to send, and the decode below
// refuses it anyway.
func DecodeJSON(r *http.Request, v any) error {
	if r.ContentLength != 0 && !isJSONBody(r) {
		return ErrNotJSON
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

// isJSONBody reports whether the request declares a JSON body. The
// registry's push notification arrives as
// application/vnd.docker.distribution.events.v1+json, so the structured
// suffix counts as much as the exact type.
func isJSONBody(r *http.Request) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return kind == "application/json" || strings.HasSuffix(kind, "+json")
}

// SameOrigin reports whether a request came from the page this daemon
// serves, rather than from another site that happens to be open in the
// same browser.
//
// It is what a CSRF token would otherwise be for. SameSite=Lax on the
// session cookie is the first line and does not reach far enough on its
// own: an app deployed here answers at app.example.com while the
// dashboard is at example.com, which makes them *same-site* and the
// cookie is sent. Anyone who can push an app to this instance could
// otherwise host a page that acts as whoever visits it.
//
// A safe method is always allowed: it changes nothing, and the answer is
// no more than the caller could read by asking for it themselves.
//
// Sec-Fetch-Site is the browser's own answer and is preferred where it
// exists; Origin is the fallback, compared by host because the scheme
// depends on whether Traefik has a certificate yet. Neither header at
// all is refused, because a browser sends one of them on every
// cross-origin request that matters and this check only ever runs on a
// request authenticated by cookie.
func SameOrigin(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
