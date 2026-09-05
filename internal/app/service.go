package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/database"
	"cubeship/internal/project"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Service holds the app use cases. Authorization is the caller's own
// role: see user.Require, and RoleToDeploy for the one place the answer
// depends on what the app is.
type Service struct {
	db       *database.DB
	projects *project.Service
	orch     *Orchestrator
	settings *settings.Service
}

func NewService(db *database.DB, projects *project.Service, orch *Orchestrator, cfg *settings.Service) *Service {
	return &Service{db: db, projects: projects, orch: orch, settings: cfg}
}

// registryHost is where apps are pushed, or "" while the instance has no
// domain. Read per call rather than captured at startup, because an
// operator configures the domain from the dashboard after installing —
// the answer changes without a restart.
func (s *Service) RegistryHost(ctx context.Context) string {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return settings.RegistryHostFor(values.Get(settings.Domain))
}

// ImageFor returns the registry path a push to this app targets, or ""
// while no domain is configured — there is nowhere to push to yet.
func (s *Service) ImageFor(ctx context.Context, a *Scoped) string {
	host := s.RegistryHost(ctx)
	if host == "" {
		return ""
	}
	return ReferenceOf(a).ImageFor(host)
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Orchestrator exposes the deploy engine for the registry webhook, which
// deploys without a caller to authorize.
func (s *Service) Orchestrator() *Orchestrator { return s.orch }

// Resolve looks up an app by reference and requires minRole of the
// caller.
func (s *Service) Resolve(ctx context.Context, caller *user.User, ref Reference, minRole user.Role) (*Scoped, error) {
	if err := user.Require(caller, minRole); err != nil {
		return nil, err
	}
	a, err := s.Repo().ScopedByReference(ctx, ref.Project, ref.Environment, ref.Name)
	if err != nil {
		return nil, ErrNotFound
	}
	// Loaded here rather than joined, so every caller that resolves an
	// app has its domains without any of them remembering to ask.
	domains, err := s.Repo().Domains(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	a.Domains = domains
	return a, nil
}

// ResolveString is Resolve for a reference that still has to be parsed.
func (s *Service) ResolveString(ctx context.Context, caller *user.User, ref string, minRole user.Role) (*Scoped, error) {
	parsed, err := ParseReference(ref)
	if err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, parsed, minRole)
}

// Create registers an app in a project's environment and returns it,
// including the registry path a push should target.
func (s *Service) Create(ctx context.Context, caller *user.User, projectSlug, envSlug, name, description string, source Source, origin Origin) (*Scoped, error) {
	if envSlug == "" {
		envSlug = project.ProductionEnvSlug
	}
	if source == "" {
		source = DefaultSource
	}
	if !source.Valid() {
		return nil, ErrUnknownSource
	}
	if err := checkOrigin(source, &origin); err != nil {
		return nil, err
	}
	// The name becomes the last path component of the app's registry
	// image reference, so it is checked before anything is looked up.
	if slug.Reserved(name) {
		return nil, slug.ErrReserved
	}
	if !slug.Valid(name) {
		return nil, slug.ErrInvalid
	}

	// Creating an app that builds is deciding that this instance will
	// execute whatever that source contains, so it takes the same role
	// deploying it does. A member creating one they could never deploy
	// would be an odd thing to allow.
	if err := user.Require(caller, RoleToDeploy(source)); err != nil {
		return nil, err
	}
	p, err := s.projects.Repo().BySlug(ctx, projectSlug)
	if err != nil {
		return nil, project.ErrNotFound
	}
	env, err := s.projects.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, project.ErrEnvironmentNotFound
	}
	ref := Reference{Project: p.Slug, Environment: env.Slug, Name: name}
	if _, err := s.Repo().Create(ctx, p.ID, env.ID, name, description, source, origin); err != nil {
		// The unique index is the authority, not a preceding lookup:
		// two concurrent creates of the same name would both pass a
		// check and the loser would surface as a 500.
		if database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return s.Repo().ScopedByReference(ctx, ref.Project, ref.Environment, ref.Name)
}

// Delete removes an app: its container first, then its rows. The order
// matters — a row deleted while the container runs leaves something
// serving traffic that nothing knows how to stop.
//
// Images already pushed stay in the registry. Reclaiming them needs a
// registry garbage collection pass, which is a separate operation.
// Update reconfigures an app: its description, the domain Traefik
// serves it at, and where its image comes from.
//
// An app is created with almost none of that, so this is where it
// becomes deployable. Changing the source to one that builds is the same
// decision as creating one that builds — this instance will execute
// whatever that repository contains — so it takes the same role, checked
// against the source being moved to rather than the one being left.
func (s *Service) Update(ctx context.Context, caller *user.User, ref Reference, description *string, source *Source, origin *Origin) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}

	// The source and its origin fields are one decision: checkOrigin
	// judges them together, and an app that names an image its source
	// ignores is exactly what it exists to refuse.
	if source != nil || origin != nil {
		next := Source(a.Source)
		if source != nil {
			next = *source
		}
		if !next.Valid() {
			return nil, ErrUnknownSource
		}
		o := Origin{Image: a.SourceImage, Repo: a.SourceRepo, Ref: a.SourceRef, Dockerfile: a.SourceDockerfile}
		if origin != nil {
			o = *origin
		}
		if err := checkOrigin(next, &o); err != nil {
			return nil, err
		}
		if err := user.Require(caller, RoleToDeploy(next)); err != nil {
			return nil, err
		}
		source, origin = &next, &o
	}

	if _, err := s.Repo().Update(ctx, a.ID, description, source, origin); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleMember)
}

func (s *Service) Delete(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.orch.Retire(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("stop the app's container: %w", err)
	}
	return a, s.Repo().Delete(ctx, a.ID)
}

// DeleteAppsInProject and DeleteAppsInEnvironment are
// project.AppTeardown: what deleting a project or an environment calls
// to take the apps under it out of service first.
//
// They take no caller. Authorization happened above — nobody reaches
// these without having been allowed to delete the thing that contains
// them — and re-deriving it here from an app's own organization would
// ask a question already answered.
func (s *Service) DeleteAppsInProject(ctx context.Context, projectID int64) error {
	apps, err := s.Repo().ListForProject(ctx, projectID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

func (s *Service) DeleteAppsInEnvironment(ctx context.Context, environmentID int64) error {
	apps, err := s.Repo().ListForEnvironment(ctx, environmentID)
	if err != nil {
		return err
	}
	return s.deleteAll(ctx, apps)
}

// deleteAll retires and removes apps one at a time, stopping at the
// first failure.
//
// Stopping is deliberate. Retire already refuses to return success while
// a container it could not remove is still running, and carrying on past
// that would delete the rest of the rows and leave whoever asked with a
// container nothing on the instance names any more. What has been
// deleted stays deleted, and the delete above is refused — so a retry
// resumes rather than starting over.
func (s *Service) deleteAll(ctx context.Context, apps []*App) error {
	for _, a := range apps {
		if err := s.orch.Retire(ctx, a.ID); err != nil {
			return fmt.Errorf("stop app %q's container: %w", a.Name, err)
		}
		if err := s.Repo().Delete(ctx, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// List returns every app on the instance.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Scoped, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	apps, err := s.Repo().ListScoped(ctx)
	if err != nil {
		return nil, err
	}
	return s.withDomains(ctx, apps)
}

// withDomains fills in what each app answers at, in one query rather
// than one per app.
func (s *Service) withDomains(ctx context.Context, apps []*Scoped) ([]*Scoped, error) {
	ids := make([]int64, 0, len(apps))
	for _, a := range apps {
		ids = append(ids, a.ID)
	}
	byApp, err := s.Repo().DomainsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range apps {
		a.Domains = byApp[a.ID]
	}
	return apps, nil
}

// Env returns the app's own variables plus the effective environment its
// container actually runs with: the project's, overridden by the
// environment's, overridden by the app's, each value labelled with the
// level that won it.
//
// Without this there is no way to see what an app is configured with —
// which is what made replacing the whole map so easy to do by accident.
func (s *Service) Env(ctx context.Context, caller *user.User, ref Reference) (envvar.Map, []envvar.Resolved, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	p, err := s.projects.Repo().ByID(ctx, a.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.projects.EnvironmentRepo().ByID(ctx, a.EnvironmentID)
	if err != nil {
		return nil, nil, err
	}
	resolved := envvar.Resolve(
		envvar.Layer{Source: envvar.SourceProject, Vars: p.Env},
		envvar.Layer{Source: envvar.SourceEnvironment, Vars: e.Env},
		envvar.Layer{Source: envvar.SourceApp, Vars: a.Env})
	return a.Env, resolved, nil
}

// requireEnvRole is the role writing an app's environment takes.
//
// For an app that runs a published image it is a member's: the variables
// are the container's, read by whatever that image already does with
// them.
//
// For an app that builds, they are also *build input*. Railpack reads
// the environment to work out how to build the repository, and turns
// RAILPACK_INSTALL_CMD, RAILPACK_BUILD_CMD and RAILPACK_START_CMD into
// the commands the build runs — inside the privileged builder, on this
// host. Writing them is therefore the same act as building, and it takes
// the same role: an app's own variables win the merge over its
// environment's and its project's, so a member who could write them
// could decide what an admin's app builds and runs.
func (s *Service) requireEnvRole(caller *user.User, a *Scoped) error {
	return user.Require(caller, RoleToDeploy(Source(a.Source)))
}

// SetEnv replaces the app's own variables, deleting any key not present.
// They are layered on top of (and override) its environment's and
// project's.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, ref Reference, env envvar.Map) (*Scoped, error) {
	// Resolved as a member first, so someone who cannot write this app's
	// environment still gets the answer an unknown app gets rather than
	// two different refusals. See requireEnvRole for the check after.
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().SetEnv(ctx, a.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, ref Reference, set envvar.Map, unset []string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnvRole(caller, a); err != nil {
		return nil, err
	}
	return a, s.Repo().MergeEnv(ctx, a.ID, set, unset)
}

// Deploy accepts a redeploy of an app from a tag already pushed to its
// registry path, and returns the deployment recording it. The work runs
// detached — see Orchestrator.Start — so the caller can stop waiting
// without stopping the deploy.
func (s *Service) Deploy(ctx context.Context, caller *user.User, ref Reference, tag string) (*Scoped, *Deployment, error) {
	if tag == "" {
		tag = "latest"
	}
	// Resolved as a member first, so someone outside the organization
	// gets the same 404 an unknown app gets rather than learning it
	// exists. The source's own requirement is checked after.
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	if err := user.Require(caller, RoleToDeploy(Source(a.Source))); err != nil {
		return nil, nil, err
	}
	deployment, err := s.orch.Start(ctx, a.ID, tag)
	if err != nil {
		return nil, nil, err
	}
	return a, deployment, nil
}

// WaitForDeployment blocks until a deployment finishes or ctx is done.
// Abandoning the wait does not abandon the deploy.
func (s *Service) WaitForDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.WaitFor(ctx, a.ID, deploymentID)
}

// Deployment reads one of an app's deployments.
func (s *Service) Deployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	d, err := s.Repo().DeploymentByID(ctx, a.ID, deploymentID)
	if err != nil {
		return nil, ErrDeploymentNotFound
	}
	return d, nil
}

// MaxDeploymentHistory bounds how much of an app's history a listing
// returns. Deploy history grows without limit; nobody reads past the
// recent ones.
const MaxDeploymentHistory = 50

// Deployments returns an app's recent deploy history, newest first.
func (s *Service) Deployments(ctx context.Context, caller *user.User, ref Reference) ([]*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.Repo().ListDeployments(ctx, a.ID, MaxDeploymentHistory)
}

// Logs returns an app's container output. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (s *Service) Logs(ctx context.Context, caller *user.User, ref Reference, tail string) (io.ReadCloser, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.Logs(ctx, a.ID, tail)
}

// DeployOnPush starts a deploy for every app in an organization that
// builds from this repository at this branch, and reports how many.
//
// It authorizes nothing, deliberately: the caller is a webhook GitHub
// signed, not a person. What stands in for a role check is the
// signature, and the fact that this instance only receives events for
// the repositories its own installation has been given.
//
// An app with no ref of its own deploys on a push to any branch. That is
// what "track the default branch" means without anybody having to name
// it, and naming a ref is how you opt out.
func (s *Service) DeployOnPush(ctx context.Context, fullName, branch string) (int, error) {
	apps, err := s.Repo().BuildingFromRepository(ctx, fullName, branch)
	if err != nil {
		return 0, err
	}

	started := 0
	for _, a := range apps {
		// The branch, not a tag: for a building source the deploy's
		// argument is which commit-ish to build.
		if _, err := s.orch.Start(ctx, a.ID, branch); err != nil {
			// One app refusing must not stop the others. A repository
			// with four apps on it should deploy the three that can.
			log.Printf("deploy on push: %s: %v", ReferenceOf(a), err)
			continue
		}
		started++
	}
	return started, nil
}

// AddDomain gives an app another name to answer at.
//
// The host is normalised the way a browser sends one — lowercase, no
// trailing dot — because that is what Traefik matches against, and a
// name stored differently is a name that never matches.
//
// Port 0 means DefaultPort. See Domain.Port for why nothing reads it
// off the image.
func (s *Service) AddDomain(ctx context.Context, caller *user.User, ref Reference, host string, port int) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}

	host = NormalizeHost(host)
	if host == "" {
		return nil, ErrDomainRequired
	}
	if !ValidHost(host) {
		return nil, ErrBadHost
	}
	if taken, err := s.instanceOwnsHost(ctx, host); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrHostIsTheInstance
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}

	if _, err := s.Repo().AddDomain(ctx, a.ID, host, port); err != nil {
		// The unique index is the authority. Two apps answering at one
		// name would give Traefik two answers, and which it picked
		// would be a detail of label ordering.
		if database.IsUniqueViolation(err) {
			return nil, ErrDomainTaken
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}

// instanceOwnsHost reports whether a name is one the daemon already
// routes to itself.
//
// The domain is the dashboard and the API; registry.<domain> is the
// registry. Both get Traefik routers of their own, and a router the
// unique index knows nothing about is exactly the collision it cannot
// catch — the app would be created, and one of the two would quietly
// stop answering after a deploy.
func (s *Service) instanceOwnsHost(ctx context.Context, host string) (bool, error) {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return false, err
	}
	domain := settings.APIHostFor(values.Get(settings.Domain))
	if domain == "" {
		return false, nil
	}
	return host == NormalizeHost(domain) ||
		host == NormalizeHost(settings.RegistryHostFor(values.Get(settings.Domain))), nil
}

// SetDomainPort changes what one of an app's names reaches.
func (s *Service) SetDomainPort(ctx context.Context, caller *user.User, ref Reference, domainID int64, port int) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if port < 0 || port > 65535 {
		return nil, ErrBadPort
	}
	if err := s.Repo().SetDomainPort(ctx, a.ID, domainID, port); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}

// RemoveDomain takes a name off an app.
//
// The container keeps the labels it was created with, so the name goes
// on being served until the app is redeployed. That is the same rule
// every other routing change follows, and it is why this does not stop
// anything by itself.
func (s *Service) RemoveDomain(ctx context.Context, caller *user.User, ref Reference, domainID int64) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, user.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.Repo().RemoveDomain(ctx, a.ID, domainID); err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, ErrDomainNotFound
		}
		return nil, err
	}
	return s.Resolve(ctx, caller, ref, user.RoleAdmin)
}
