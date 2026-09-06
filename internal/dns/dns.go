// Package dns writes the records this instance needs — an app's name
// pointed at this host, a challenge proving a domain is yours — through
// the account whose DNS you already manage.
//
// It holds no accounts of its own. It used to: a DNS provider was a
// label and a secret, which is exactly what a credential is, so the
// same Cloudflare token lived here and an AWS key lived here *and* in
// the registries. Both moved to internal/credential, and what is left
// in this package is the part that is actually about DNS — two provider
// clients and the operations they have in common.
//
// A credential id is therefore what every operation here is addressed
// by. Whether the account can do DNS at all is the credential module's
// question, asked once in resolve.
package dns

import (
	"errors"
	"strings"

	"cubeship/internal/credential"
)

// Credential is what a provider client authenticates with.
//
// An alias rather than a type of its own, and deliberately: there is
// one credential in this daemon now, and a second name for it here
// would be the beginning of a second copy of it.
type Credential = credential.Credential

// State is what a live probe of a provider found. The three answers are
// three different jobs for whoever reads them: nothing, re-authenticate,
// or wait for someone else's API to come back.
type State string

const (
	StateAvailable    State = "available"
	StateUnauthorized State = "unauthorized"
	StateUnreachable  State = "unreachable"
)

// Status is one credential's answer, and why.
type Status struct {
	State State `json:"state"`
	// Detail is the reason, for anything but available.
	Detail string `json:"detail,omitempty"`
}

// Zone is one domain a provider holds, and Record one entry in it. They
// are the same shape whichever provider answered, because what the
// dashboard shows is the same either way.
type Zone struct {
	// ID is the provider's own identifier for the zone, which is what
	// every later call addresses it by. It is not derivable from the
	// name — Route 53 assigns one, Cloudflare assigns another — so it
	// travels with the zone rather than being looked up again.
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Record is one entry in a zone.
//
// Values is a list because a record *is* a list at both providers: two
// A records for one name are one record set with two values at Route 53,
// and two rows at Cloudflare. Flattening them to one value each would
// make a round trip lose half of what was there.
type Record struct {
	// ID identifies the record at Cloudflare, which addresses records by
	// id. Route 53 has no such thing — a record set is addressed by its
	// name and type — so it is empty there, and nothing outside the
	// provider's own file may depend on it.
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Values  []string `json:"values"`
	TTL     int      `json:"ttl"`
	Proxied bool     `json:"proxied,omitempty"`
}

// RecordTypes are the ones Cubeship offers.
//
// Not every type both providers support — that would be a long list
// including several nobody sets by hand. These are the ones an operator
// pointing a name at this host, or proving they own it, actually needs.
var RecordTypes = []string{"A", "AAAA", "CNAME", "TXT", "MX", "NS", "SRV", "CAA"}

// ValidRecordType reports whether a type is one of the above.
func ValidRecordType(t string) bool {
	for _, known := range RecordTypes {
		if known == t {
			return true
		}
	}
	return false
}

// DefaultTTL is what a record gets when nobody says. Five minutes is
// short enough that a mistake is recoverable within a coffee and long
// enough that a resolver is not asking constantly.
const DefaultTTL = 300

var (
	// ErrNotFound is a zone or a credential this instance cannot see.
	// It stays here rather than moving with the accounts, because a
	// provider client raises it for a zone that is gone.
	ErrNotFound          = errors.New("not found")
	ErrRecordTypeUnknown = errors.New("that is not a record type Cubeship sets")
	ErrRecordNotFound    = errors.New("no such record in that zone")
)

// NormalizeName renders a record name the way both providers store one:
// lowercase, with no trailing dot. Route 53 answers with the dot and
// Cloudflare without it, and a listing that showed both spellings would
// make one zone look like two.
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// Conflicts reports whether a record already at a name stands in the way
// of one being written there.
//
// A CNAME cannot share a name with anything else — that is DNS, not a
// provider's quirk — so it excludes every other type and every other
// type excludes it. Two records of the same type are in the way for a
// different reason: writing one means replacing what is there, not
// adding a second answer beside it.
//
// Both providers have to obey this before writing. Neither of them will
// do it for you: Cloudflare refuses the create, and Route 53 refuses the
// change set, each with a message about the record already there rather
// than about the one you asked for.
func Conflicts(existing, wanted string) bool {
	return existing == wanted || existing == "CNAME" || wanted == "CNAME"
}
