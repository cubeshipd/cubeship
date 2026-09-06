// Package extregistry owns the credentials Cubeship uses to pull from a
// registry it does not run — Docker Hub, GitHub, DigitalOcean, ECR.
//
// It is deliberately not internal/registry, which is Cubeship's own
// registry: who may push to it and what a push triggers. These are the
// opposite direction, and confusing the two would be easy.
package extregistry

import (
	"errors"
	"strings"
	"time"
)

// Provider is which registry a credential is for. It decides what is
// asked of whoever adds one, and how the daemon authenticates with it.
type Provider string

const (
	// ProviderGeneric is any registry that takes a username and a
	// password: Docker Hub, GitHub, Gitea, a Harbor someone runs.
	ProviderGeneric Provider = "generic"

	// ProviderDigitalOcean is DigitalOcean's container registry. It is a
	// generic registry underneath — an email and an API token — but the
	// host is fixed and the registry's name is a path segment, so
	// asking for those two separately beats asking for a URL.
	ProviderDigitalOcean Provider = "digitalocean"

	// ProviderAWS is Elastic Container Registry, and it is the one that
	// is not a password at all: what is stored is an access key, and the
	// token Docker logs in with is fetched from AWS and lasts hours.
	ProviderAWS Provider = "aws"
)

func (p Provider) Valid() bool {
	switch p {
	case ProviderGeneric, ProviderDigitalOcean, ProviderAWS:
		return true
	}
	return false
}

// DigitalOceanHost is where every DigitalOcean registry lives. The part
// that differs between accounts is the first path segment, not the host.
const DigitalOceanHost = "registry.digitalocean.com"

// Credential is one login, held by an organization and reusable by every
// app in it that pulls from that host.
//
// The secret is stored as given, not hashed: a hash cannot be sent to a
// registry, nor signed with. Nothing reads it back out through the API.
type Credential struct {
	ID int64

	// CredentialID is the account this registry authenticates as. The
	// secret is not here any more: one AWS key reaches ECR and Route 53
	// both, and storing it once is the point of internal/credential.
	CredentialID int64

	// Provider, Username and Password come from that credential and are
	// filled on read. Nothing writes them through this type — an edit
	// to a login is an edit to the credential, in one place, and every
	// registry using it follows.
	Provider Provider
	// Host is the registry, and the identity: one per host per
	// organization.
	Host string
	// Namespace is the path segment between the host and the image
	// where the provider has one — DigitalOcean's registry name. Not
	// part of matching, which is by host.
	Namespace string
	// Region is AWS's. An ECR host carries the account id and the
	// region; the account id is discovered, the region is asked for.
	Region string
	// Username and Password mean different things per provider: a login
	// for a generic registry, an access key id and secret for AWS.
	Username  string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewLogin is a login typed in place of choosing a stored account.
//
// A credential is a **convenience, not a prerequisite**: somebody adding
// their first registry has no stored account yet, and being sent to
// another screen to make one before they can do the thing they came to
// do is the tail wagging the dog. So the login can be typed here, and
// the account is created from it — which means it turns up under
// Credentials and can be picked next time, for the second registry or
// for DNS.
type NewLogin struct {
	Provider Provider
	// Label is what the stored account is called. Derived from the
	// registry when empty, because somebody adding a registry is not
	// necessarily thinking about naming an account.
	Label    string
	Username string
	Password string
}

var (
	ErrNothingToUpdate    = errors.New("nothing to change: give a credential, a registry name, or both")
	ErrTwoLogins          = errors.New("pick a stored account or type a login, not both")
	ErrDifferentProvider  = errors.New("that credential is for a different provider — a registry's host was derived from the account it was created with, so it cannot move to another kind of account")
	ErrCredentialRequired = errors.New("no login: pick the account this registry authenticates as, or type one")
	ErrUnknownProvider    = errors.New(`provider must be "generic", "digitalocean" or "aws"`)
	ErrHostRequired       = errors.New("host is required")
	ErrUsernameRequired   = errors.New("username is required")
	ErrPasswordRequired   = errors.New("password is required")
	ErrNamespaceRequired  = errors.New("the registry name is required — it is what follows registry.digitalocean.com/ in an image path")
	ErrRegionRequired     = errors.New("an AWS region is required: an ECR registry lives in one, and it cannot be guessed")
	ErrHostTaken          = errors.New("this organization already has a credential for that registry")
	ErrNotFound           = errors.New("no such registry credential")
)

// NormalizeHost reduces what someone types to the host an image
// reference actually carries, so a credential entered as
// "https://registry.digitalocean.com/" still matches
// "registry.digitalocean.com/acme/api".
//
// Docker Hub is the exception every registry client has to make: an
// image written "acme/api" has no host at all, and the daemon's own
// name for the Hub is index.docker.io. Both spellings land there.
func NormalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	switch host {
	case "docker.io", "registry-1.docker.io", "hub.docker.com":
		return DockerHub
	}
	return host
}

// DockerHub is the host an image with no registry in its name comes
// from.
const DockerHub = "index.docker.io"

// HostOf returns the registry an image reference names, in the same
// spelling NormalizeHost produces — so a credential's host and an
// image's host are comparable.
//
// The first segment is the registry only if it looks like an address: it
// has a dot, a colon, or is "localhost". Otherwise the whole reference
// is a Docker Hub repository ("acme/api", "postgres").
func HostOf(image string) string {
	first, _, found := strings.Cut(image, "/")
	if !found {
		return DockerHub
	}
	if !strings.ContainsAny(first, ".:") && first != "localhost" {
		return DockerHub
	}
	return strings.ToLower(first)
}

// ImageRef is how one image inside a repository is named for a delete.
//
// Two ways, and which one is available is not a preference. A tagged
// image is addressed by its tag, so removing it leaves the other tags on
// the same image alone. An untagged one has nothing but its digest —
// which is also why it cannot be given a stand-in name in a listing: a
// stand-in is a name two of them would share and no delete could reach.
type ImageRef struct {
	Tag    string
	Digest string
}

// Named reports whether the reference identifies anything at all.
func (r ImageRef) Named() bool { return r.Tag != "" || r.Digest != "" }

// In renders the reference the way someone would write it, for an error.
func (r ImageRef) In(repository string) string {
	if r.Tag != "" {
		return repository + ":" + r.Tag
	}
	return repository + "@" + r.Digest
}
