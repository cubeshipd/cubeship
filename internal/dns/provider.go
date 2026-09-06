package dns

import (
	"errors"
	"time"

	"cubeship/internal/credential"
)

// Provider is whose API this instance writes records through. It is a
// fact about the *use*, not about the secret: one AWS access key writes
// Route 53 records here and pulls from ECR through a registry row, and
// storing "aws" on the key would be deciding in advance which of those
// it is for.
type Provider string

const (
	// ProviderAWS is Route 53, reached with an IAM access key.
	ProviderAWS Provider = "aws"

	// ProviderCloudflare is Cloudflare's API, reached with a token.
	ProviderCloudflare Provider = "cloudflare"
)

// spec is what differs between one provider and another, and it is only
// ever three sentences: what to call it, and what its login's two
// halves are called for whoever is typing one in.
type spec struct {
	name string
	// usernameLabel is what the first half is called, and empty where
	// the secret is a single value — a token has no name beside it, and
	// a field asking for one would be a field with no right answer.
	usernameLabel string
	passwordLabel string
	hint          string
}

var specs = map[Provider]spec{
	ProviderAWS: {
		name:          "Amazon Web Services",
		usernameLabel: "Access key ID",
		passwordLabel: "Secret access key",
		hint:          "An IAM user's access key, allowed to read and change records in the zones this instance should write to.",
	},
	ProviderCloudflare: {
		name:          "Cloudflare",
		passwordLabel: "API token",
		hint:          "A token with Zone:Read and DNS:Edit on the zones this instance should write to. A Global API Key works and reaches your whole account, which is more than this needs.",
	},
}

// Providers is every provider this release can write records through,
// in the order a form should offer them.
func Providers() []Provider { return []Provider{ProviderAWS, ProviderCloudflare} }

func (p Provider) Valid() bool           { _, ok := specs[p]; return ok }
func (p Provider) Name() string          { return specs[p].name }
func (p Provider) UsernameLabel() string { return specs[p].usernameLabel }
func (p Provider) PasswordLabel() string { return specs[p].passwordLabel }
func (p Provider) Hint() string          { return specs[p].hint }

// Account is one provider reached with one credential — the whole of
// what "a DNS provider" is here.
type Account struct {
	ID       int64
	Provider Provider

	// CredentialID is the secret this account authenticates with. The
	// secret is not stored here: the same key may be doing two other
	// jobs on this instance, and it is stored once.
	CredentialID int64

	// Label, Username and Password come from that credential and are
	// filled on read, so a provider client is handed one value with a
	// login on it and never learns where the login lives.
	Label    string
	Username string
	Password string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AsCredential is what a provider client authenticates with.
func (a *Account) AsCredential() *Credential {
	return &credential.Credential{
		ID: a.CredentialID, Label: a.Label,
		Username: a.Username, Password: a.Password,
	}
}

// NewLogin is a login typed in place of choosing a stored credential.
//
// A credential is a **convenience, not a prerequisite**: somebody adding
// their first DNS provider has no stored secret yet, and being sent to
// another screen to make one before they can do the thing they came to
// do is the tail wagging the dog. So the login can be typed here, and
// the credential is created from it — which means it turns up under
// Credentials and can be picked next time, for a registry or for a
// second provider.
type NewLogin struct {
	// Label is what the stored credential is called. Derived from the
	// provider when empty, because somebody connecting Cloudflare is
	// not necessarily thinking about naming a secret.
	Label    string
	Username string
	Password string
}

var (
	// ErrUnknownProvider is a provider this release has no client for.
	// Refused at creation: storing one would be an account that can
	// never write a record.
	ErrUnknownProvider = errors.New(`provider must be "aws" or "cloudflare"`)

	// ErrTwoLogins is both a stored credential and a typed one, which
	// has no obvious reading.
	ErrTwoLogins = errors.New("pick a stored credential or type a login, not both")

	// ErrCredentialRequired is neither.
	ErrCredentialRequired = errors.New("no login: pick the credential this provider authenticates with, or type one")

	// ErrAccountTaken is the same credential offered twice for the same
	// provider — two rows nothing could tell apart.
	ErrAccountTaken = errors.New("this instance already reaches that provider with that credential")

	// ErrInstanceDNS refuses deleting the provider this instance writes
	// its own records through. Removing it silently would leave the
	// domain screen pointed at nothing.
	ErrInstanceDNS = errors.New("this instance writes its own records through this provider — point the instance's domain elsewhere first")
)
