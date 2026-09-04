// Package settings holds the instance-wide configuration an operator
// changes after installing, rather than before starting.
//
// Cubeship is installed with one command and reached by IP; the domain it
// serves apps on, and the address Let's Encrypt sends expiry notices to,
// are things the operator fills in afterwards. Keeping them in the
// database rather than the environment is what lets the daemon boot
// knowing neither.
package settings

import "errors"

// Keys. Every setting is optional: the daemon runs, in a reduced form,
// with none of them set.
const (
	// Domain is the base domain, e.g. "example.com". The API is served
	// at api.<domain> and the registry at registry.<domain>.
	//
	// Without it there is no registry to push to: a remote `docker push`
	// needs a public name, and the registry's token realm has to be an
	// address the client can reach.
	Domain = "domain"

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
	GitHubAppID         = "github_app_id"
	GitHubAppSlug       = "github_app_slug"
	GitHubPrivateKey    = "github_private_key"
	GitHubWebhookSecret = "github_webhook_secret"
)

// Secret reports whether a key holds a credential. A credential is
// writable and never readable: an endpoint that hands one back turns
// every read of the configuration into a way out for it.
func Secret(key string) bool {
	return key == GitHubPrivateKey || key == GitHubWebhookSecret
}

// ErrUnknownKey is returned for a key this version does not define.
// Settings are a fixed set, not arbitrary storage.
var ErrUnknownKey = errors.New("unknown setting")

// ErrSuperAdminOnly guards writes: instance configuration is the VPS
// operator's, not an organization's.
var ErrSuperAdminOnly = errors.New("forbidden: only a super-admin can change instance settings")

// known is every key this version recognizes, with what it is for.
var known = map[string]string{
	Domain:              "Base domain. The API is served at api.<domain> and the registry at registry.<domain>; both must resolve to this host.",
	ACMEEmail:           "Contact address for Let's Encrypt. Certificates are only issued once this is set.",
	GitHubAppID:         "The numeric id of the GitHub App this instance acts as.",
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

// HasDomain reports whether a registry and TLS-capable routing are
// possible at all.
func (v Values) HasDomain() bool { return v[Domain] != "" }

// HasTLS reports whether certificates can be issued. Both a domain and a
// contact address are needed: Let's Encrypt will not register an account
// without one, and there is nothing to get a certificate for without the
// other.
func (v Values) HasTLS() bool { return v.HasDomain() && v[ACMEEmail] != "" }

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

// APIHostFor is where the daemon's API is reached through Traefik.
func APIHostFor(domain string) string {
	if domain == "" {
		return ""
	}
	return "api." + domain
}
