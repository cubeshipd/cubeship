// Package dns holds the DNS providers an organization manages its
// records through.
//
// Cubeship already asks an operator to point a name at this host — that
// is what a domain and an app's host are for — and until now the pointing
// happened in somebody else's control panel. These credentials are what
// let it happen here instead.
//
// A credential belongs to the **organization**, not to an app or a
// domain: one Cloudflare account holds every zone in it, and rotating a
// token should be one edit rather than one per name. Two credentials for
// the same provider are allowed — an account can legitimately be split
// across two — so what is unique is the credential's own label, not the
// provider.
package dns

import (
	"errors"
	"strings"
	"time"
)

// Provider is who runs the DNS.
type Provider string

const (
	ProviderCloudflare Provider = "cloudflare"
	ProviderRoute53    Provider = "route53"
)

// Valid reports whether a provider is one this daemon can act through.
// A value it cannot act on is refused at creation: accepting one would
// let someone store a credential that can never resolve anything.
func (p Provider) Valid() bool {
	return p == ProviderCloudflare || p == ProviderRoute53
}

// Credential is one account's worth of DNS access.
//
// The two providers authenticate differently and the difference is not
// cosmetic: Cloudflare takes one API token, and Route 53 takes an access
// key that is two halves. Rather than two tables, both land in the same
// two columns — Route 53's key id in Username, its secret in Password —
// because everything above this treats them the same way and only the
// call to the provider knows which is which.
type Credential struct {
	ID       int64    `json:"id"`
	OrgID    int64    `json:"-"`
	Provider Provider `json:"provider"`

	// Label is what distinguishes two credentials for one provider. It
	// is the only thing here someone chooses freely, and it exists
	// because "the Cloudflare one" stops identifying anything the moment
	// there are two.
	Label string `json:"label"`

	// Username is Route 53's access key id, and empty for Cloudflare —
	// whose token is a single value with no name attached to it.
	Username string `json:"username,omitempty"`

	// Password is the secret half: Cloudflare's API token, or Route 53's
	// secret access key. Stored as given and never returned — a provider
	// takes the secret itself, so a hash could not be sent, and an
	// endpoint that handed it back would turn every read of the list
	// into a way out for it.
	Password string `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

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
	ErrNotFound          = errors.New("no such DNS provider")
	ErrProviderRequired  = errors.New("say which provider this is for: cloudflare or route53")
	ErrLabelRequired     = errors.New("the label is required — it is what tells two accounts apart")
	ErrPasswordRequired  = errors.New("the API token or secret access key is required")
	ErrUsernameRequired  = errors.New("Route 53 needs an access key id as well as a secret")
	ErrLabelTaken        = errors.New("this organization already has a DNS provider with that label")
	ErrRecordTypeUnknown = errors.New("that is not a record type Cubeship sets")
	ErrRecordNotFound    = errors.New("no such record in that zone")
)

// NormalizeLabel trims a label to what is stored. It is a human label
// rather than an identifier, so it keeps its case and its spaces — the
// only thing worth removing is what someone did not mean to type.
func NormalizeLabel(label string) string {
	return strings.TrimSpace(label)
}

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
