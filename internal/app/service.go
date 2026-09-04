package app

import (
	"context"
	"fmt"
	"io"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/project"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// LocalRegistryHost is where the daemon pulls app images from. The
// registry container publishes 127.0.0.1:5000 precisely so the daemon's
// own pulls stay on loopback: pulling the public registry.<domain> name
// would hairpin out to the VPS's public IP and require a valid ACME
// certificate to already exist, which must never be a precondition for
// deploying.
const LocalRegistryHost = "127.0.0.1:5000"

// Service holds the app use cases. Authorization is always the owning
// organization's answer.
type Service struct {
	db       *database.DB
	orgs     *org.Service
	projects *project.Service
	orch     *Orchestrator
	settings *settings.Service
}

func NewService(db *database.DB, orgs *org.Service, projects *project.Service, orch *Orchestrator, cfg *settings.Service) *Service {
	return &Service{db: db, orgs: orgs, projects: projects, orch: orch, settings: cfg}
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

// Resolve looks up an app by reference and requires minRole in its
// owning organization, folding "doesn't exist" and "not authorized" into
// the same error so a response never reveals that a given app exists.
func (s *Service) Resolve(ctx context.Context, caller *user.User, ref Reference, minRole org.Role) (*Scoped, error) {
	a, err := s.Repo().ScopedByReference(ctx, ref.Org, ref.Project, ref.Environment, ref.Name)
	if err != nil {
		return nil, ErrNotFound
	}
	if !s.orgs.Authorize(ctx, caller, a.OrgID, minRole) {
		return nil, ErrNotFound
	}
	return a, nil
}

// ResolveString is Resolve for a reference that still has to be parsed.
func (s *Service) ResolveString(ctx context.Context, caller *user.User, ref string, minRole org.Role) (*Scoped, error) {
	parsed, err := ParseReference(ref)
	if err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, parsed, minRole)
}

// Create registers an app in a project's environment and returns it,
// including the registry path a push should target.
func (s *Service) Create(ctx context.Context, caller *user.User, orgSlug, projectSlug, envSlug, name, domain string) (*Scoped, error) {
	if envSlug == "" {
		envSlug = project.ProductionEnvSlug
	}
	// The name becomes a path component of the app's registry image
	// reference (registry.<domain>/<org>/<name>), so it is checked before
	// anything is looked up.
	if !slug.Valid(name) {
		return nil, slug.ErrInvalid
	}

	o, err := s.orgs.Resolve(ctx, caller, orgSlug, org.RoleMember)
	if err != nil {
		return nil, err
	}
	p, err := s.projects.Repo().BySlug(ctx, o.ID, projectSlug)
	if err != nil {
		return nil, project.ErrNotFound
	}
	env, err := s.projects.EnvironmentRepo().BySlug(ctx, p.ID, envSlug)
	if err != nil {
		return nil, project.ErrEnvironmentNotFound
	}
	ref := Reference{Org: o.Slug, Project: p.Slug, Environment: env.Slug, Name: name}
	if _, err := s.Repo().Create(ctx, o.ID, p.ID, env.ID, name, domain); err != nil {
		// The unique index is the authority, not a preceding lookup:
		// two concurrent creates of the same name would both pass a
		// check and the loser would surface as a 500.
		if database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}
	return s.Repo().ScopedByReference(ctx, ref.Org, ref.Project, ref.Environment, ref.Name)
}

// Delete removes an app: its container first, then its rows. The order
// matters — a row deleted while the container runs leaves something
// serving traffic that nothing knows how to stop.
//
// Images already pushed stay in the registry. Reclaiming them needs a
// registry garbage collection pass, which is a separate operation.
func (s *Service) Delete(ctx context.Context, caller *user.User, ref Reference) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, err
	}
	if err := s.orch.Retire(ctx, a.ID); err != nil {
		return nil, fmt.Errorf("stop the app's container: %w", err)
	}
	return a, s.Repo().Delete(ctx, a.ID)
}

// List returns every app caller can see. The organization filter is
// applied in SQL, not in Go: a member of one organization should never
// have every other tenant's rows read on their behalf.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Scoped, error) {
	if caller == nil {
		return nil, user.ErrUnauthenticated
	}
	if caller.IsSuperAdmin {
		return s.Repo().ListScoped(ctx)
	}
	memberships, err := s.orgs.Repo().ListMembershipsForUser(ctx, caller.ID)
	if err != nil {
		return nil, err
	}
	orgIDs := make([]int64, 0, len(memberships))
	for _, m := range memberships {
		orgIDs = append(orgIDs, m.OrgID)
	}
	return s.Repo().ListScopedForOrgs(ctx, orgIDs)
}

// Env returns the app's own variables plus the effective environment its
// container actually runs with: the project's, overridden by the
// environment's, overridden by the app's, each value labelled with the
// level that won it.
//
// Without this there is no way to see what an app is configured with —
// which is what made replacing the whole map so easy to do by accident.
func (s *Service) Env(ctx context.Context, caller *user.User, ref Reference) (envvar.Map, []envvar.Resolved, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
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
		envvar.Layer{Source: envvar.SourceApp, Vars: a.Env},
	)
	return a.Env, resolved, nil
}

// SetEnv replaces the app's own variables, deleting any key not present.
// They are layered on top of (and override) its environment's and
// project's.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, ref Reference, env envvar.Map) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return a, s.Repo().SetEnv(ctx, a.ID, env)
}

// MergeEnv adds or overwrites the given variables and removes the unset
// ones, leaving every other key untouched.
func (s *Service) MergeEnv(ctx context.Context, caller *user.User, ref Reference, set envvar.Map, unset []string) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
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
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, nil, err
	}
	image := s.ImageFor(ctx, a)
	if image == "" {
		return nil, nil, ErrNoRegistry
	}
	deployment, err := s.orch.Start(ctx, a.ID, LocalPullRef(image, tag))
	if err != nil {
		return nil, nil, err
	}
	return a, deployment, nil
}

// WaitForDeployment blocks until a deployment finishes or ctx is done.
// Abandoning the wait does not abandon the deploy.
func (s *Service) WaitForDeployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.WaitFor(ctx, a.ID, deploymentID)
}

// Deployment reads one of an app's deployments.
func (s *Service) Deployment(ctx context.Context, caller *user.User, ref Reference, deploymentID int64) (*Deployment, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
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
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.Repo().ListDeployments(ctx, a.ID, MaxDeploymentHistory)
}

// Logs returns an app's container output. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (s *Service) Logs(ctx context.Context, caller *user.User, ref Reference, tail string) (io.ReadCloser, error) {
	a, err := s.Resolve(ctx, caller, ref, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.Logs(ctx, a.ID, tail)
}

// LocalPullRef rewrites a public image reference
// (registry.<domain>/<org-slug>/<app>) into the loopback-published
// reference the daemon actually pulls. Only the repository part is kept;
// the host is replaced. See LocalRegistryHost.
func LocalPullRef(image, tag string) string {
	repo := image
	if i := strings.Index(image, "/"); i >= 0 {
		repo = image[i+1:]
	}
	return LocalRegistryHost + "/" + repo + ":" + tag
}
