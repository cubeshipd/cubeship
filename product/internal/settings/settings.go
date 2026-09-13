// Package settings holds the instance-wide configuration an operator
// changes after installing, rather than before starting.
//
// Cubeship is installed with one command and reached by IP; the domain it
// serves apps on, and the address Let's Encrypt sends expiry notices to,
// are things the operator fills in afterwards. Keeping them in the
// database rather than the environment is what lets the daemon boot
// knowing neither.
package settings

import (
	"errors"
	"strings"
	"time"
)

// Keys. Every setting is optional: the daemon runs, in a reduced form,
// with none of them set.
const (
	// Domain is the instance's own name — `cubeship.example.com` is what
	// the setup flow offers, and the whole instance lives at it rather
	// than beside it.
	//
	// A subdomain rather than the operator's apex, because it is what
	// makes the DNS one decision instead of a growing list: an A record
	// on the name and one wildcard under it cover the dashboard, the
	// registry and everything Cubeship grows later, and the operator
	// hands over one subdomain rather than a wildcard on the domain
	// their mail runs on.
	//
	// Nothing enforces the shape. An apex works, and so does any name
	// that resolves here — the default is a recommendation, not a rule.
	//
	// The API is served
	// at api.<domain> and the registry at registry.<domain>.
	//
	// Without it there is no registry to push to: a remote `docker push`
	// needs a public name, and the registry's token realm has to be an
	// address the client can reach.
	Domain = "domain"

	// PublicIP overrides what this host believes its own address to be.
	//
	// Empty is the normal case: a VPS holds its public address on its
	// own interface and the daemon reads it there. This exists for the
	// host that does not — behind NAT, or with more than one address
	// and the wrong one chosen — where the records Cubeship writes
	// would otherwise point somewhere nothing answers.
	PublicIP = "public_ip"

	// DNSProviderID is which stored DNS credential writes this
	// instance's own records, or empty for an operator who keeps their
	// DNS somewhere Cubeship cannot reach.
	//
	// It is a setting rather than something chosen at each use, because
	// the records it writes are the instance's own and there is one
	// right answer for them at a time.
	DNSProviderID = "dns_provider_id"

	// AutoUpdateAt is the time of day this instance updates itself,
	// as `HH:MM` in the instance's own timezone. Empty is off, which
	// is what every instance is until somebody says otherwise.
	//
	// A time rather than an interval, because what somebody is choosing
	// is when the instance is allowed to be briefly unusable — and
	// "every 24 hours from whenever you turned it on" is not a thing
	// anybody can plan around.
	AutoUpdateAt = "auto_update_at"

	// AutoUpdateTimezone is what that time is in, as an IANA name. It
	// defaults to UTC, which is what a server's clock is and what
	// nobody means: 03:00 is chosen because it is the middle of *your*
	// night, so the timezone is the half of the answer that makes the
	// number mean anything.
	AutoUpdateTimezone = "auto_update_timezone"

	// ACMEEmail is the contact address Let's Encrypt registers.
	//
	// Without it Traefik has no certificate resolver, so apps are served
	// over plain HTTP. Apps deployed before it is set keep their old
	// routing until they are redeployed.
	ACMEEmail = "acme_email"

	// The GitHub App this instance acts as. One App per instance: it is
	// registered by whoever runs the VPS, and organizations install it
	// on their own accounts.
	//
	// GitHubPrivateKey and GitHubWebhookSecret are credentials. Nothing
	// reads them back out through the API — the settings endpoint
	// reports whether they are set, never what they are.
	// The App's OAuth half. It is what proves who is connecting an
	// installation: GitHub answers "which installations can this person
	// reach", and an id outside that answer is one they do not own.
	//
	// The secret is write-only, like the private key. The client id is
	// not a secret — it appears in a URL the browser is sent to — but it
	// is stored beside its half rather than derived.
	GitHubClientID     = "github_client_id"
	GitHubClientSecret = "github_client_secret"

	GitHubAppID         = "github_app_id"
	GitHubAppSlug       = "github_app_slug"
	GitHubPrivateKey    = "github_private_key"
	GitHubWebhookSecret = "github_webhook_secret"
)

// Secret reports whether a key holds a credential. A credential is
// writable and never readable: an endpoint that hands one back turns
// every read of the configuration into a way out for it.
func Secret(key string) bool {
	return key == GitHubPrivateKey || key == GitHubWebhookSecret ||
		key == GitHubClientSecret
}

// ErrUnknownKey is returned for a key this version does not define.
// Settings are a fixed set, not arbitrary storage.
var ErrUnknownKey = errors.New("unknown setting")

// ErrSuperAdminOnly guards writes: instance configuration is the VPS
// operator's, not an organization's.
var ErrSuperAdminOnly = errors.New("forbidden: only a super-admin can change instance settings")

// known is every key this version recognizes, with what it is for.
var known = map[string]string{
	Domain:              "Base domain. The dashboard and the API are served at <domain> and the registry at registry.<domain>; both must resolve to this host.",
	ACMEEmail:           "Contact address for Let's Encrypt. Optional: certificates are issued as soon as there is a domain.",
	AutoUpdateAt:        "When this instance updates itself, as HH:MM. Empty is off. Only stable releases, and only when there is one: an instance already on the newest does nothing.",
	AutoUpdateTimezone:  "What auto_update_at is in, as an IANA timezone like Europe/Lisbon. Empty is UTC, which is a server's clock rather than anybody's night.",
	PublicIP:            "What this instance's DNS records should point at. Empty means work it out — the address the dashboard is opened at, or the machine's own — which is right on a VPS and has no answer behind NAT. A private address is never the answer: one of those in a record is a domain that stops resolving.",
	DNSProviderID:       "Which stored DNS credential writes this instance's own records. Empty means the operator keeps their DNS elsewhere and writes them by hand.",
	GitHubAppID:         "The numeric id of the GitHub App this instance acts as.",
	GitHubClientID:      "The App's OAuth client id, which is what someone connecting an installation is sent to GitHub with.",
	GitHubClientSecret:  "The App's OAuth client secret. Write-only.",
	GitHubAppSlug:       "The App's slug, which is what its install page is addressed by.",
	GitHubPrivateKey:    "The App's private key, in PEM. Write-only.",
	GitHubWebhookSecret: "The secret GitHub signs its webhooks with. Write-only.",
}

// Describe returns what a key is for, and whether it is a key at all.
func Describe(key string) (string, bool) {
	description, ok := known[key]
	return description, ok
}

// Values is every setting, by key. A key that has never been set is
// absent rather than empty.
type Values map[string]string

// Get returns a setting, or "" when it has never been set.
func (v Values) Get(key string) string { return v[key] }

// HasGitHub reports whether this instance can act as a GitHub App at
// all. Without it, building a private repository and deploying on a push
// are both impossible.
func (v Values) HasGitHub() bool {
	return v.Get(GitHubAppID) != "" && v.Get(GitHubPrivateKey) != ""
}

// HasGitHubOAuth reports whether the App can be installed anywhere but
// the account that owns it.
//
// The two go together, which is why one flag answers for both. An App
// registered before Cubeship asked for OAuth on install was also
// registered private, and a private GitHub App can only be installed on
// its owner's account — so it can never reach an organization. There is
// no way to change either from here: both are decided when the App is
// created, so the answer to an old App is a new one.
func (v Values) HasGitHubOAuth() bool {
	return v.Get(GitHubClientID) != "" && v.Get(GitHubClientSecret) != ""
}

// HasDomain reports whether a registry and TLS-capable routing are
// possible at all.
func (v Values) HasDomain() bool { return v[Domain] != "" }

// HasTLS reports whether certificates can be issued, which takes a
// domain and nothing else: Let's Encrypt registers an account without a
// contact address, and Traefik passes an empty one through. The address
// is where expiry notices would go, and Let's Encrypt stopped sending
// those — it is a courtesy, not a condition.
func (v Values) HasTLS() bool { return v.HasDomain() }

// The names derived from the base domain. They are computed here rather
// than stored so that changing the domain changes them everywhere at
// once.

// RegistryHostFor is where apps are pushed: registry.<domain>.
func RegistryHostFor(domain string) string {
	if domain == "" {
		return ""
	}
	return "registry." + domain
}

// wildcardDNS are the services that answer for every name under an
// IP-embedding address: <anything>.<a-b-c-d>.sslip.io resolves to
// a.b.c.d, at any depth, with nothing registered anywhere.
//
// install.sh falls back to one of these when nobody gave a domain, which
// is what makes a fresh instance reachable by name — and, because the
// wildcard is not only for the instance, what lets an app be given an
// address without owning a domain at all.
var wildcardDNS = []string{".sslip.io", ".nip.io"}

// ResolvesEveryName reports whether every name under this domain already
// points at this host.
//
// It is the difference between an address that works the moment it is
// added and one that needs a record written first, which is the only
// thing anybody has to be told when they are offered a name for an app.
func ResolvesEveryName(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	for _, suffix := range wildcardDNS {
		if strings.HasSuffix(domain, suffix) {
			return true
		}
	}
	return false
}

// APIHostFor is where the daemon is reached through Traefik, which is
// the domain itself.
//
// It used to be `api.<domain>`, from when the dashboard and the API were
// two things at two addresses. They are one address — the daemon serves
// /api and proxies the rest — so a second name for the same server was a
// name that had to be explained.
func APIHostFor(domain string) string { return domain }

// ValidTimeOfDay reports whether s is `HH:MM` on a 24-hour clock, or
// empty — which is how automatic updating is turned off.
//
// Checked here rather than left to the scheduler, because a time it
// cannot parse is a setting that silently does nothing: the instance
// would sit there never updating, and the screen would show the value
// somebody typed.
func ValidTimeOfDay(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h, m := s[:2], s[3:]
	for _, part := range []string{h, m} {
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	hour := int(h[0]-'0')*10 + int(h[1]-'0')
	minute := int(m[0]-'0')*10 + int(m[1]-'0')
	return hour < 24 && minute < 60
}

// ValidTimezone reports whether s is an IANA name this machine knows,
// or empty — which is UTC.
//
// Asked of the machine rather than checked against a list: the list is
// the tzdata on the host, and a name this daemon cannot load is one the
// scheduler could not use whatever a table here said about it.
func ValidTimezone(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.LoadLocation(s)
	return err == nil
}

// ErrBadTimeOfDay and ErrBadTimezone are the two ways the update
// schedule is refused.
var (
	ErrBadTimeOfDay = errors.New("an update time is HH:MM on a 24-hour clock, or empty to turn it off")
	ErrBadTimezone  = errors.New("that is not a timezone this machine knows: give an IANA name like Europe/Lisbon, or leave it empty for UTC")
)
