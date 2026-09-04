// Package slug defines the one shape every user-chosen identifier in
// Cubeship has to take: organization, project, environment and app names
// alike.
package slug

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
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

// Valid reports whether s is an acceptable slug.
func Valid(s string) bool {
	return pattern.MatchString(s)
}

// Title turns a slug into the display name a resource starts life with:
// "public-api" becomes "Public Api".
//
// It exists because the slug is the only thing anyone is asked for. A
// slug is a permanent, machine-shaped identifier and a name is a label
// someone changes freely, so asking for both at creation asked twice
// for the same idea — and the answers drifted. The daemon derives one
// and lets it be edited afterwards.
//
// The result is a guess, and deliberately a dumb one: "api" becomes
// "Api", not "API". Anything cleverer would be a dictionary, and the
// display name is a field the owner can simply correct.
func Title(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		parts[i] = string(unicode.ToUpper(r[0])) + string(r[1:])
	}
	return strings.Join(parts, " ")
}
