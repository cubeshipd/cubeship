package app

import (
	"fmt"
	"strings"

	"cubeship/internal/project"
	"cubeship/internal/slug"
)

// Reference identifies one app: the organization, project and
// environment that contain it, plus its name.
//
// An app's name is unique only within its environment, so a name alone
// no longer identifies anything. The reference's string form —
// "acme/web/production/myapp" — is also the app's registry repository
// path, so what you push to and what you name it are the same thing.
type Reference struct {
	Project     string
	Environment string
	Name        string
}

// String is the canonical three-part form.
func (r Reference) String() string {
	return r.Project + "/" + r.Environment + "/" + r.Name
}

// ImageFor returns the registry path a push to this app targets.
func (r Reference) ImageFor(registryHost string) string {
	return registryHost + "/" + r.String()
}

// ParseReference reads "project/environment/app". Two parts are
// accepted as a shorthand for the "production" environment, which is the
// one every project is guaranteed to have.
func ParseReference(s string) (Reference, error) {
	parts := strings.Split(strings.Trim(s, "/"), "/")

	var ref Reference
	switch len(parts) {
	case 3:
		ref = Reference{Project: parts[0], Environment: parts[1], Name: parts[2]}
	case 2:
		ref = Reference{Project: parts[0], Environment: project.ProductionEnvSlug, Name: parts[1]}
	default:
		return Reference{}, fmt.Errorf(
			"%q is not an app reference: expected project/environment/app, or project/app for %s",
			s, project.ProductionEnvSlug)
	}

	// Every part is a slug, and rejecting a bad one here keeps a
	// malformed reference from ever reaching a registry path or a
	// Traefik router name.
	for label, part := range map[string]string{
		"project": ref.Project, "environment": ref.Environment, "app name": ref.Name,
	} {
		if !slug.Valid(part) {
			return Reference{}, fmt.Errorf("%s %q %s", label, part, slug.ErrInvalid)
		}
	}
	return ref, nil
}

// ReferenceOf is the reference of an app already loaded with its scope.
func ReferenceOf(a *Scoped) Reference {
	return Reference{Project: a.ProjectSlug, Environment: a.EnvironmentSlug, Name: a.Name}
}

// resourceName is the Docker and Traefik identifier for an app: unique
// across the instance, and legal in both.
//
// Slashes are not allowed in a container name or a Traefik router name,
// so the reference's separators become dashes. Every part is already a
// slug, so the result cannot collide with another app's unless the
// references themselves collide — which the unique index prevents.
func resourceName(ref Reference) string {
	return "cubeship-" + strings.Join(
		[]string{ref.Project, ref.Environment, ref.Name}, "-")
}

// InternalHost is the name every container on this instance reaches
// this app at — whichever machine it runs on, and however many copies
// of it are running.
//
// **A container's name is not an address here, and that is the whole
// reason this exists.** An app's carries the deployment that created it
// — `cubeship-web-production-api-1421` — because a machine has to be
// able to answer "am I already running this deployment" from the name
// alone. Anything that wrote that down would be pointing at a container
// that stops existing on the next deploy. So the stable part of it is
// attached to the container as a network **alias**, on the local bridge
// and on the mesh both, and that is what one app calls another by.
//
// It is the shape `cubeship-db-pg` and `cubeship-s3-media` already
// have, and for the same reason: an app reaching a database, a store or
// another app is one thing somebody learns, not three.
//
// **This is what a public name cannot do from inside.** An app that
// asks for its neighbour's domain is a request leaving the box for a
// record that points back at it, and a host that does not hairpin its
// own NAT answers nothing at all — which is a failure that looks like
// the other app being down.
//
// The port is the app's own: this is DNS and nothing else, so there is
// no proxy in the path, no TLS terminated for it and no health check
// taken into account. Every copy of the app holds the alias and the
// Engine's DNS answers with all of them, so a scaled-out app is spread
// over without anything here balancing it.
func InternalHost(ref Reference) string { return resourceName(ref) }
