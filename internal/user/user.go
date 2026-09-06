// Package user owns identities and the credentials they authenticate
// with: the User and APIKey entities, their persistence, the use cases
// that manage them, and the HTTP and MCP surfaces those are reached
// through.
//
// It also owns the one authorization question on the instance. There is
// no tenant boundary above it — Cubeship runs one instance on one VPS —
// so what a caller may do is a property of the account, and Require is
// where every other module asks.
package user

import (
	"errors"
	"time"
)

// Role is what an account may do on this instance.
//
// The two are not a hierarchy of seniority but a line between two kinds
// of act: running an image someone already published, and turning source
// into an image on this host. See app.RoleToDeploy.
type Role string

const (
	// RoleAdmin can add accounts, create projects and environments,
	// configure the instance, build source into images, and everything
	// a member can.
	RoleAdmin Role = "admin"
	// RoleMember can create, deploy, configure and read the logs of
	// apps that run published images.
	RoleMember Role = "member"
)

// Valid reports whether r is a role this instance recognizes.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleMember }

// User is one identity on this Cubeship instance.
type User struct {
	ID        int64
	Username  string
	Role      Role
	CreatedAt time.Time
}

// Is reports whether u holds at least min. An admin satisfies both
// checks; a member only satisfies RoleMember.
func (u *User) Is(min Role) bool {
	if u == nil {
		return false
	}
	return min == RoleMember || u.Role == RoleAdmin
}

// Require is the authorization every module calls. It answers with the
// error the caller should see rather than a bool, so the two refusals
// stay distinct: nobody signed in at all, and somebody signed in who
// lacks the role.
func Require(caller *User, min Role) error {
	if caller == nil {
		return ErrUnauthenticated
	}
	if !caller.Is(min) {
		return ErrForbidden
	}
	return nil
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
// explicitly chosen: a new account's first key. A key created through
// the "additional key" endpoint always carries a caller-chosen name
// instead — "mcp", "laptop", whatever distinguishes it.
const DefaultAPIKeyName = "default"

var (
	// ErrUnauthenticated is what the service returns when it is handed
	// no caller at all — a bug in the transport wiring rather than a
	// user error, but never a panic.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden is returned when the caller is signed in but lacks
	// the role an action requires. Unlike a missing resource, this is
	// said plainly: they already know the thing exists, and hiding it
	// would only confuse them.
	ErrForbidden = errors.New("forbidden: this requires the admin role")

	// ErrInvalidRole reports a role string that is neither admin nor
	// member.
	ErrInvalidRole = errors.New(`role must be "admin" or "member"`)

	// ErrUsernameTaken reports that the username was claimed by a
	// concurrent request while this one was working.
	ErrUsernameTaken = errors.New("that username was just taken; try again")

	// ErrNoSuchUser is a username that is not an account here.
	ErrNoSuchUser = errors.New("no such account")

	// ErrCannotRemoveYourself refuses deleting the account making the
	// request. Whoever meant to leave has to be removed by somebody
	// else, and an admin who deletes themselves mid-session is the one
	// mistake nothing on the instance can undo.
	ErrCannotRemoveYourself = errors.New("you cannot delete the account you are signed in as")

	// ErrLastAdmin refuses removing or demoting the only admin. An
	// instance with no admin can never configure itself again, and
	// nothing in the API could put one back — setup is closed the
	// moment the first account exists.
	ErrLastAdmin = errors.New("this is the only admin on the instance")
)
