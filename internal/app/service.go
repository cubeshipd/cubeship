package app

import (
	"context"
	"io"
	"strings"

	"cubeship/internal/envvar"
	"cubeship/internal/org"
	"cubeship/internal/platform/database"
	"cubeship/internal/project"
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
	db           *database.DB
	orgs         *org.Service
	projects     *project.Service
	orch         *Orchestrator
	registryHost string
}

func NewService(db *database.DB, orgs *org.Service, projects *project.Service, orch *Orchestrator, registryHost string) *Service {
	return &Service{db: db, orgs: orgs, projects: projects, orch: orch, registryHost: registryHost}
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Orchestrator exposes the deploy engine for the registry webhook, which
// deploys without a caller to authorize.
func (s *Service) Orchestrator() *Orchestrator { return s.orch }

// Resolve looks up an app by name and requires minRole in its owning
// organization, folding "doesn't exist" and "not authorized" into the
// same error so a response never reveals that a given app name is taken.
func (s *Service) Resolve(ctx context.Context, caller *user.User, name string, minRole org.Role) (*Scoped, error) {
	a, err := s.Repo().ScopedByName(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	if !s.orgs.Authorize(ctx, caller, a.OrgID, minRole) {
		return nil, ErrNotFound
	}
	return a, nil
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
	if _, err := s.Repo().ByName(ctx, name); err == nil {
		return nil, ErrAlreadyExists
	}

	image := s.registryHost + "/" + o.Slug + "/" + name
	if _, err := s.Repo().Create(ctx, o.ID, p.ID, env.ID, name, domain, image); err != nil {
		return nil, err
	}
	return s.Repo().ScopedByName(ctx, name)
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

// SetEnv replaces the app's own variables. They are layered on top of
// (and override) its environment's and project's.
func (s *Service) SetEnv(ctx context.Context, caller *user.User, name string, env envvar.Map) (*Scoped, error) {
	a, err := s.Resolve(ctx, caller, name, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return a, s.Repo().SetEnv(ctx, a.ID, env)
}

// Deploy redeploys an app from a tag already pushed to its registry path.
func (s *Service) Deploy(ctx context.Context, caller *user.User, name, tag string) (*Scoped, error) {
	if tag == "" {
		tag = "latest"
	}
	a, err := s.Resolve(ctx, caller, name, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return a, s.orch.Deploy(ctx, a.Name, LocalPullRef(a.Image, tag))
}

// Logs returns an app's container output. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (s *Service) Logs(ctx context.Context, caller *user.User, name, tail string) (io.ReadCloser, error) {
	a, err := s.Resolve(ctx, caller, name, org.RoleMember)
	if err != nil {
		return nil, err
	}
	return s.orch.Logs(ctx, a.Name, tail)
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
