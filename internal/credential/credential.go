// Package credential holds the accounts this instance is wired to, and
// the secrets that reach them.
//
// It exists because one secret is one secret. An AWS access key is the
// same key whether Route 53 is writing a record with it, ECR is being
// pulled from with it, or a bucket is being read with it — and before
// this, each of those asked for it separately. Three copies of one
// credential is three places to rotate it and three chances to miss
// one.
//
// **A credential and the use of it are different things.** What is
// stored here is the account and the secret. Where a use needs
// configuration of its own — which host a registry answers at, which
// region, which bucket — that lives with the use, in a row that names
// the credential it authenticates with. A use with no configuration at
// all needs no row: managing DNS through an account *is* holding a
// credential for it, which is why there is no longer a table of DNS
// providers.
//
// What a credential can be used for is a property of the provider, not
// something anybody ticks. Where a vendor issues two different kinds of
// secret — DigitalOcean's API token and its Spaces keys are not the
// same string — those are two providers, because that is what they
// are.
package credential

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Provider is whose account a credential reaches. It decides what is
// asked of whoever adds one, what the two halves of the secret mean,
// and what the credential may be used for.
type Provider string

const (
	// ProviderAWS is an IAM access key: two halves, and the one
	// credential that genuinely covers everything this instance does
	// with a cloud account.
	ProviderAWS Provider = "aws"

	// ProviderCloudflare is an API token. One value, with no name
	// attached to it.
	ProviderCloudflare Provider = "cloudflare"

	// ProviderDigitalOcean is a personal access token. It reaches the
	// container registry, which takes the token as both halves of a
	// docker login.
	//
	// It does not reach Spaces. Those are S3-style keys DigitalOcean
	// issues separately, and when object storage lands they will be
	// their own provider rather than a second secret hidden inside this
	// one.
	ProviderDigitalOcean Provider = "digitalocean"

	// ProviderGeneric is a username and a password for a registry
	// nobody here has special knowledge of: Docker Hub, GitHub, a
	// Harbor someone runs. It is not an account with an API behind it,
	// which is why it can only be used for one thing.
	ProviderGeneric Provider = "generic"
)

// Capability is something a credential can be used for. It is derived
// from the provider — see the package comment.
type Capability string

const (
	CapabilityDNS      Capability = "dns"
	CapabilityRegistry Capability = "registry"
)

// spec is everything that differs between one provider and another.
type spec struct {
	// name is the provider as a person calls it.
	name string
	// capabilities are what a credential of this provider may be used
	// for, in a fixed order so a listing does not shuffle.
	capabilities []Capability
	// usernameLabel is what the first half of the secret is called, and
	// empty when this provider's secret has no first half — a token is
	// one value, and asking for a name to go with it would be asking
	// for something that does not exist.
	usernameLabel string
	// passwordLabel is what the secret half is called. Every provider
	// has one.
	passwordLabel string
	// hint is the sentence under the fields: where to get one, and what
	// it needs to be allowed to do.
	hint string
}

var specs = map[Provider]spec{
	// One access key, both jobs. This is the case the whole module
	// exists for: Route 53 and ECR are reached with the same IAM key,
	// and they used to be stored as two credentials under two
	// different provider names — "route53" and "aws" — which is how an
	// operator ended up typing one secret twice.
	ProviderAWS: {
		name:          "Amazon Web Services",
		capabilities:  []Capability{CapabilityDNS, CapabilityRegistry},
		usernameLabel: "Access key ID",
		passwordLabel: "Secret access key",
		hint:          "An IAM user's access key. The same key reaches Route 53 and ECR, so it needs whichever of those you mean to use it for — and nothing else.",
	},
	ProviderCloudflare: {
		name:          "Cloudflare",
		capabilities:  []Capability{CapabilityDNS},
		passwordLabel: "API token",
		hint:          "A token with Zone:Read and DNS:Edit on the zones this instance should write to. A Global API Key works and reaches your whole account, which is more than this needs.",
	},
	// Registry only, and not because the token cannot do DNS — it can.
	// This daemon has no DigitalOcean DNS client, and a capability the
	// daemon cannot act on is a credential somebody would store for a
	// job it can never do. It goes in the day the client does.
	ProviderDigitalOcean: {
		name:          "DigitalOcean",
		capabilities:  []Capability{CapabilityRegistry},
		passwordLabel: "API token",
		hint:          "A personal access token with read and write. It does not reach Spaces — those are separate keys.",
	},
	ProviderGeneric: {
		name:          "Registry login",
		capabilities:  []Capability{CapabilityRegistry},
		usernameLabel: "Username",
		passwordLabel: "Password or access token",
		hint:          "For a registry with no special support here: Docker Hub, GitHub, a Harbor of your own. Prefer an access token over the account password where the registry offers one.",
	},
}

// Providers is every provider this release knows, in the order a form
// should offer them. TestEveryProviderHasASpec pins it against specs.
func Providers() []Provider {
	return []Provider{ProviderAWS, ProviderCloudflare, ProviderDigitalOcean, ProviderGeneric}
}

// Capabilities is every capability this release defines, so a client
// can name the filter it wants without hard-coding the list.
func Capabilities() []Capability {
	return []Capability{CapabilityDNS, CapabilityRegistry}
}

func (p Provider) Valid() bool { _, ok := specs[p]; return ok }

// Name is the provider as a person calls it.
func (p Provider) Name() string { return specs[p].name }

// Capabilities are what a credential of this provider may be used for.
func (p Provider) Capabilities() []Capability { return specs[p].capabilities }

// Can reports whether this provider may be used for c.
func (p Provider) Can(c Capability) bool {
	for _, have := range specs[p].capabilities {
		if have == c {
			return true
		}
	}
	return false
}

// UsernameLabel is what the first half of the secret is called, or
// empty for a provider whose secret is one value.
func (p Provider) UsernameLabel() string { return specs[p].usernameLabel }

// PasswordLabel is what the secret half is called.
func (p Provider) PasswordLabel() string { return specs[p].passwordLabel }

// Hint is the sentence under the fields.
func (p Provider) Hint() string { return specs[p].hint }

// NeedsUsername reports whether this provider's secret has a first
// half. A token has none, and a field asking for one would be a field
// with no right answer.
func (p Provider) NeedsUsername() bool { return specs[p].usernameLabel != "" }

// Credential is one account, and the secret that reaches it.
type Credential struct {
	ID       int64
	Provider Provider

	// Label is what tells two credentials apart, and the only thing
	// here somebody chooses freely. It exists because "the AWS one"
	// stops identifying anything the moment there are two — a personal
	// account and a company's, staging and production.
	Label string

	// Username is the first half of the secret, and empty for a
	// provider whose secret is one value. What it means is the
	// provider's business: an access key id, a registry login.
	Username string

	// Password is the secret half. Stored as given and never returned:
	// a provider takes the secret itself, so a hash could not be sent
	// to one, and an endpoint that handed it back would turn every read
	// of the list into a way out for it.
	Password string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Use is one thing that depends on a credential, for saying what breaks
// before it is deleted.
type Use struct {
	// What kind of thing it is — "registry", "instance DNS".
	Kind string
	// How a person recognises it.
	Name string
}

// String is how a use is named in the sentence that refuses a delete.
// Trimmed, because some uses are a kind with nothing to distinguish
// them — there is only one "instance's own DNS".
func (u Use) String() string { return strings.TrimSpace(u.Kind + " " + u.Name) }

var (
	// ErrNotFound covers both "no such credential" and an id from
	// another instance's URL.
	ErrNotFound = errors.New("credential not found")

	// ErrLabelTaken reports a label already in use. Labels are how a
	// person picks one out of a list, so two the same would make the
	// list unreadable rather than merely ambiguous.
	ErrLabelTaken = errors.New("a credential with that label already exists")

	// ErrLabelRequired is an empty label. There is nothing else to call
	// one by: the secret cannot be shown and the provider is not unique.
	ErrLabelRequired = errors.New("a label is required — it is how you tell two credentials apart")

	// ErrUnknownProvider is a provider this release cannot act through.
	// Refused at creation: storing one would be a credential that can
	// never be used for anything.
	ErrUnknownProvider = errors.New("unknown provider")

	// ErrUnknownCapability is a filter naming something no provider
	// does.
	ErrUnknownCapability = errors.New("unknown capability")

	// ErrUsernameRequired is a missing first half where the provider
	// has one.
	ErrUsernameRequired = errors.New("this provider's secret has two halves and both are required")

	// ErrPasswordRequired is a missing secret. Every provider has one.
	ErrPasswordRequired = errors.New("the secret is required")

	// ErrUsernameNotAllowed is a first half given to a provider whose
	// secret is a single value. Refused rather than dropped: silently
	// ignoring something somebody typed is how a credential comes out
	// different from what they thought they stored.
	ErrUsernameNotAllowed = errors.New("this provider's secret is a single value, so there is no name to go with it")

	// ErrCannot reports a credential asked to do something its provider
	// does not do.
	ErrCannot = errors.New("this provider cannot be used for that")

	// ErrNoDependants reports that the modules using credentials were
	// never wired in, so a delete cannot know what it would break.
	// Refusing is the only safe answer.
	ErrNoDependants = errors.New("cannot delete: the modules that use credentials are not wired in")

	// ErrInUse refuses deleting a credential something still depends
	// on. The alternative is a registry that cannot authenticate and a
	// deploy that fails for a reason nobody can see from the screen
	// they were on.
	ErrInUse = errors.New("something is still using this credential")
)

// CannotError says which capability a provider lacks, and what it does
// instead — a refusal that only says no is a refusal somebody has to go
// and look up.
func CannotError(p Provider, c Capability) error {
	have := make([]string, 0, len(p.Capabilities()))
	for _, capability := range p.Capabilities() {
		have = append(have, string(capability))
	}
	return fmt.Errorf("%w: %s is for %s, not %s",
		ErrCannot, p.Name(), strings.Join(have, " and "), c)
}

// InUseError names what is still depending on it.
func InUseError(uses []Use) error {
	names := make([]string, 0, len(uses))
	for _, u := range uses {
		names = append(names, u.String())
	}
	return fmt.Errorf("%w: %s. Point those elsewhere first, or remove them",
		ErrInUse, strings.Join(names, ", "))
}
