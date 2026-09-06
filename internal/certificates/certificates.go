// Package certificates reports the TLS certificates this instance holds,
// and the names that should have one and do not.
//
// Nothing here issues anything. Traefik does that, through the ACME
// resolver it is started with, and keeps what it gets in acme.json —
// which is the whole store: there is no API in front of it and no row
// about it anywhere in Cubeship. This module reads that file, parses the
// certificates in it, and lines them up against the names this instance
// actually routes.
//
// It is read-only on purpose. Renewing or deleting a certificate would
// mean editing a file Traefik owns while it runs, which is only safe
// with the container stopped — a few seconds of downtime for every app —
// and every re-issue spends one of a weekly limit shared with everyone
// else using the same registered domain. That is a decision worth making
// deliberately, not a button beside a table.
package certificates

import (
	"errors"
	"time"
)

// ErrNoStore reports an instance that has never had a certificate: no
// domain, so no resolver, so no acme.json. It is not a failure — it is
// the state a fresh install is in.
var ErrNoStore = errors.New("this instance has issued no certificates yet")

// Certificate is one certificate in the store, as anybody reading a
// table wants it. The private key is beside it in the file and is never
// read: nothing above this could do anything with it but leak it.
type Certificate struct {
	// Host is the name the certificate was issued for — Traefik's
	// "main" domain, which is the router's rule.
	Host string `json:"host"`
	// SANs are the other names on it. Cubeship asks for one name per
	// certificate, so this is normally empty; a certificate issued
	// before that, or by hand, may carry more.
	SANs []string `json:"sans,omitempty"`
	// Issuer is the CA's common name — "R11", "E5", whatever Let's
	// Encrypt is signing with this season.
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	// Serial identifies it to the CA, which is what a support thread
	// about one asks for.
	Serial string `json:"serial"`

	// App is the app served at this name, as its reference. Empty for
	// the instance's own names and for a certificate nothing serves any
	// more.
	App string `json:"app,omitempty"`
	// Instance says the name is the dashboard's or the registry's
	// rather than an app's.
	Instance bool `json:"instance,omitempty"`
	// Orphan says nothing on this instance answers at this name now.
	// The certificate stays valid and stays in the file; it is simply
	// paid for and unused.
	Orphan bool `json:"orphan,omitempty"`
}

// Expired reports whether the certificate has run out.
func (c Certificate) Expired(now time.Time) bool { return !now.Before(c.NotAfter) }

// DaysLeft is what a table shows: how long before it stops working.
// Negative once it has.
func (c Certificate) DaysLeft(now time.Time) int {
	return int(c.NotAfter.Sub(now).Hours() / 24)
}

// Why a name this instance routes has no certificate. The three are
// three different jobs for whoever reads them: configure the instance,
// deploy the app, or wait — and then look at the reason Traefik gave.
type Reason string

const (
	// ReasonNoTLS is the instance itself: no domain, or no contact
	// address, so Traefik was started with no resolver at all and asks
	// for nothing.
	ReasonNoTLS Reason = "tls_not_configured"
	// ReasonNotDeployed is a name added to an app that has not been
	// deployed since. A container keeps the labels it was created with,
	// so Traefik has never been told this name exists.
	ReasonNotDeployed Reason = "not_deployed"
	// ReasonPending is everything else: Traefik knows the name and has
	// not produced a certificate for it. A minute after a deploy that is
	// normal; an hour later it is a name that does not resolve here, or
	// a challenge that failed.
	ReasonPending Reason = "pending"
)

// Missing is a name this instance routes with no certificate behind it.
type Missing struct {
	Host string `json:"host"`
	// App is the app served there, empty for the instance's own names.
	App      string `json:"app,omitempty"`
	Instance bool   `json:"instance,omitempty"`
	Reason   Reason `json:"reason"`
	// Detail is the last thing Traefik said about this name, when it
	// said anything. It is read out of the container's log rather than
	// out of any API, so it is a quotation and not a contract: empty is
	// normal.
	Detail string `json:"detail,omitempty"`
}

// Report is the whole picture: what this instance holds, what it is
// missing, and whether it could have any of it.
type Report struct {
	// TLSEnabled is whether certificates are possible at all. False and
	// everything below is empty, which is the answer rather than a
	// failure.
	TLSEnabled bool `json:"tls_enabled"`
	// ACMEEmail is the contact Let's Encrypt has for this instance, as
	// the store recorded it — which is the one that counts, not the one
	// in the settings, if somebody changed it after registering.
	ACMEEmail    string        `json:"acme_email,omitempty"`
	Certificates []Certificate `json:"certificates"`
	Missing      []Missing     `json:"missing"`
	// TraefikSays is what Traefik has lately complained about while
	// trying to get certificates, whether or not the complaint names a
	// host this instance knows. Read out of the container's log, so it
	// is a quotation and not a contract — and often the only place the
	// real reason appears, a rate limit especially.
	TraefikSays []string `json:"traefik_says,omitempty"`
}

// ServedHost is a name this instance routes and what answers there. It
// is what the report is checked against: a certificate for a name
// nothing serves is an orphan, and a served name with no certificate is
// missing one.
type ServedHost struct {
	Host string
	// App is the app's reference, empty for the instance's own names.
	App string
	// Instance says this is the dashboard's or the registry's name.
	Instance bool
	// Deployed says a container is actually running with this name in
	// its labels. A name added and not yet deployed is one Traefik has
	// never heard of.
	Deployed bool
}
