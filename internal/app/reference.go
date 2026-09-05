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
