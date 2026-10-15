// Package slug defines the one shape every user-chosen identifier in
// Cubeship has to take: organization, project, environment and app names
// alike.
package slug

import (
	"errors"
	"regexp"
)

// pattern is kebab-case with no accents or other special characters.
//
// Organization and app name both become path components of an app's
// registry image reference (registry.<domain>/<org>/<app>), and Docker
// rejects a repository path with uppercase letters, accents, spaces or an
// extra "/" in it — a push against such a name could never work. Project
// and environment slugs carry no such constraint from Docker, but are
// held to the same shape for consistency and because they appear
// verbatim in URLs.
var pattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ErrInvalid is what every caller reports when Valid says no. The message
// is written for the person who typed the slug.
var ErrInvalid = errors.New("must be lowercase letters, digits and dashes, starting and ending with a letter or digit")

// ErrReserved is the other reason Valid says no: a slug of the right
// shape that the dashboard needs as a path segment.
var ErrReserved = errors.New(`"settings" is reserved: it is a page in the dashboard at the same address a resource of that name would have`)

// reserved are the words the dashboard uses as path segments beside a
// slug of the same shape.
//
// An app is addressed at /projects/<project>/<env>/<app>, and its
// settings at that path plus /settings. Next.js resolves a static
// segment before a dynamic one, so an app or environment actually
// called "settings" would be a resource nothing could open — the
// settings screen would answer for it instead, silently and forever.
//
// Refusing the name at creation is the only place this can be caught
// where the person who typed it is still there to type another.
var reserved = map[string]bool{"settings": true}

// Valid reports whether s is an acceptable slug.
func Valid(s string) bool {
	return pattern.MatchString(s) && !reserved[s]
}

// Reserved reports whether s is refused for being a word the dashboard
// needs. Callers use it to say *why* rather than repeating the shape
// rule at someone whose slug was the right shape.
func Reserved(s string) bool { return reserved[s] }
