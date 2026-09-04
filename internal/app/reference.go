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
	Org         string
	Project     string
	Environment string
	Name        string
}

// String is the canonical four-part form.
func (r Reference) String() string {
	return r.Org + "/" + r.Project + "/" + r.Environment + "/" + r.Name
}

// ImageFor returns the registry path a push to this app targets.
func (r Reference) ImageFor(registryHost string) string {
	return registryHost + "/" + r.String()
}

// ParseReference reads "org/project/environment/app". Three parts are
// accepted as a shorthand for the "production" environment, which is the
// one every project is guaranteed to have.
func ParseReference(s string) (Reference, error) {
	parts := strings.Split(strings.Trim(s, "/"), "/")

	var ref Reference
	switch len(parts) {
	case 4:
		ref = Reference{Org: parts[0], Project: parts[1], Environment: parts[2], Name: parts[3]}
	case 3:
		ref = Reference{Org: parts[0], Project: parts[1], Environment: project.ProductionEnvSlug, Name: parts[2]}
	default:
		return Reference{}, fmt.Errorf(
			"%q is not an app reference: expected org/project/environment/app, or org/project/app for %s",
			s, project.ProductionEnvSlug)
	}

	// Every part is a slug, and rejecting a bad one here keeps a
	// malformed reference from ever reaching a registry path or a
	// Traefik router name.
	for label, part := range map[string]string{
		"organization": ref.Org, "project": ref.Project,
		"environment": ref.Environment, "app name": ref.Name,
	} {
		if !slug.Valid(part) {
			return Reference{}, fmt.Errorf("%s %q %s", label, part, slug.ErrInvalid)
		}
	}
	return ref, nil
}

// ReferenceOf is the reference of an app already loaded with its scope.
func ReferenceOf(a *Scoped) Reference {
	return Reference{Org: a.OrgSlug, Project: a.ProjectSlug, Environment: a.EnvironmentSlug, Name: a.Name}
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
		[]string{ref.Org, ref.Project, ref.Environment, ref.Name}, "-")
}
