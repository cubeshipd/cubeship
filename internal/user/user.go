// Package user owns identities and the credentials they authenticate
// with: the User and APIKey entities, their persistence, the use cases
// that manage them, and the HTTP and MCP surfaces those are reached
// through.
//
// Everything a caller can do to their own account lives here.
// Organization membership does not — that belongs to whoever grants it;
// see internal/org.
package user

import (
	"errors"
	"time"
)

// User is one identity on this Cubeship instance.
type User struct {
	ID           int64
	Username     string
	IsSuperAdmin bool
	CreatedAt    time.Time
}

// APIKey is one credential belonging to a User. Only its hash is ever
// stored; the key itself is shown once, at creation, and never again.
type APIKey struct {
	ID         int64
	UserID     int64
	KeyHash    string
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// DefaultAPIKeyName is the name given to a key created without one
// explicitly chosen: the super-admin's bootstrap key and a new org
// user's first key. A key created through the "additional key" endpoint
// always carries a caller-chosen name instead — "mcp", "laptop",
// whatever distinguishes it.
const DefaultAPIKeyName = "default"

// ErrLastAPIKey reports that a revoke was refused because it would have
// left the caller with no way to authenticate at all.
var ErrLastAPIKey = errors.New("cannot revoke your only remaining API key")

// ErrUnauthenticated is what the service returns when it is handed no
// caller at all — a bug in the transport wiring rather than a user error,
// but never a panic.
var ErrUnauthenticated = errors.New("unauthenticated")
