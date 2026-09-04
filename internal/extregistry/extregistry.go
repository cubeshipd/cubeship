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

// Credential is one login, held by an organization and reusable by every
// app in it that pulls from that host.
//
// The password is stored as given, not hashed: a hash cannot be sent to
// a registry. Only a super-admin or an organization admin can read one
// back, and the read endpoints never return it at all.
type Credential struct {
	ID        int64
	OrgID     int64
	Name      string
	Host      string
	Username  string
	Password  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var (
	ErrNameRequired     = errors.New("name is required")
	ErrHostRequired     = errors.New("host is required")
	ErrUsernameRequired = errors.New("username is required")
	ErrPasswordRequired = errors.New("password is required")
	ErrHostTaken        = errors.New("this organization already has a credential for that registry")
	ErrNameTaken        = errors.New("this organization already has a credential with that name")
	ErrNotFound         = errors.New("no such registry credential")
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
