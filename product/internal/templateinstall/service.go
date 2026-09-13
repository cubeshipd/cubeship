package templateinstall

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/datastore"
	"cubeship/internal/envvar"
	"cubeship/internal/objectstore"
	"cubeship/internal/project"
	"cubeship/internal/slug"
	"cubeship/internal/user"
	"cubeship/template"
)

// What an install creates things through. Each is the module's own
// service; they are interfaces so the order and the undoing can be tested
// without a database or a Docker behind them.

type Projects interface {
	Resolve(ctx context.Context, caller *user.User, projectSlug string, minRole user.Role) (*project.Project, error)
	ResolveEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string, minRole user.Role) (*project.Environment, error)
	Create(ctx context.Context, caller *user.User, projectSlug string) (*project.Project, *project.Environment, error)
	CreateEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*project.Environment, error)
	Delete(ctx context.Context, caller *user.User, projectSlug string) (*project.Project, error)
	DeleteEnvironment(ctx context.Context, caller *user.User, projectSlug, envSlug string) (*project.Environment, error)
}

type Apps interface {
	Resolve(ctx context.Context, caller *user.User, ref app.Reference, minRole user.Role) (*app.Scoped, error)
	Create(ctx context.Context, caller *user.User, projectSlug, envSlug, name string, source app.Source, origin app.Origin) (*app.Scoped, error)
	Update(ctx context.Context, caller *user.User, ref app.Reference, source *app.Source, origin *app.Origin, health *string, limits *app.Limits, auto *app.Autoscale, place *app.Placement) (*app.Scoped, error)
	AddDomain(ctx context.Context, caller *user.User, ref app.Reference, host string, port int) (*app.Scoped, error)
	MergeEnv(ctx context.Context, caller *user.User, ref app.Reference, set envvar.Map, unset []string) (*app.Scoped, error)
	Deploy(ctx context.Context, caller *user.User, ref app.Reference, tag string) (*app.Scoped, *app.Deployment, error)
	WaitForDeployment(ctx context.Context, caller *user.User, ref app.Reference, deploymentID int64) (*app.Deployment, error)
	Delete(ctx context.Context, caller *user.User, ref app.Reference) (*app.Scoped, error)
	HostTaken(ctx context.Context, host string) (bool, error)
}

type Datastores interface {
	Resolve(ctx context.Context, caller *user.User, name string, minRole user.Role) (*datastore.Datastore, error)
	Create(ctx context.Context, caller *user.User, spec datastore.Spec) (*datastore.Datastore, error)
	Credentials(ctx context.Context, caller *user.User, name string) (datastore.Credentials, error)
	Attach(ctx context.Context, caller *user.User, name, appRef, prefix string) (*datastore.Datastore, error)
	Delete(ctx context.Context, caller *user.User, name string) (*datastore.Datastore, error)
}

type ObjectStores interface {
	Resolve(ctx context.Context, caller *user.User, name string, minRole user.Role) (*objectstore.Store, error)
	Create(ctx context.Context, caller *user.User, spec objectstore.ManagedSpec) (*objectstore.Store, error)
	Credentials(ctx context.Context, caller *user.User, name string) (objectstore.Credentials, error)
	CreateBucket(ctx context.Context, caller *user.User, name, bucket string) error
	Attach(ctx context.Context, caller *user.User, name, appRef, bucket, prefix string) (*objectstore.Store, error)
	Delete(ctx context.Context, caller *user.User, name string) (*objectstore.Store, error)
}

// Users finds who started an install, to undo it as them after a restart.
type Users interface {
	ByID(ctx context.Context, id int64) (*user.User, error)
}

// Records is where installs are kept. *Repository is one.
type Records interface {
	Create(ctx context.Context, in *Install) (*Install, error)
	Save(ctx context.Context, in *Install) error
	ByID(ctx context.Context, id int64) (*Install, error)
	List(ctx context.Context, limit int) ([]*Install, error)
	Running(ctx context.Context) ([]*Install, error)
}

// RoleToInstall is admin: an install creates databases, and a template's
// app may build a repository on this host.
const RoleToInstall = user.RoleAdmin

type Service struct {
	records    Records
	projects   Projects
	apps       Apps
	datastores Datastores
	stores     ObjectStores
	users      Users
	version    string

	mu      sync.RWMutex
	catalog Catalog

	wg sync.WaitGroup

	// Poll is how often a wait for a database or a store looks again.
	Poll time.Duration
	// Timeout is the longest an install may run before it is undone.
	Timeout time.Duration
}

func NewService(records Records, projects Projects, apps Apps, datastores Datastores, stores ObjectStores, users Users, catalog Catalog, version string) *Service {
	return &Service{
		records: records, projects: projects, apps: apps, datastores: datastores, stores: stores, users: users,
		catalog: catalog, version: version,
		Poll: 2 * time.Second, Timeout: 30 * time.Minute,
	}
}

// SetCatalog replaces where templates come from. For tests.
func (s *Service) SetCatalog(c Catalog) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.catalog = c
}

func (s *Service) source() (Catalog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.catalog == nil {
		return nil, ErrCatalogUnavailable
	}
	return s.catalog, nil
}

// Wait blocks until every install this process started has finished.
// For tests, and for a daemon shutting down.
func (s *Service) Wait() { s.wg.Wait() }

// Templates is a page of the catalog.
func (s *Service) Templates(ctx context.Context, caller *user.User, query url.Values) (json.RawMessage, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.List(ctx, query)
}

// Template is one template, with its README, file and manifest.
func (s *Service) Template(ctx context.Context, caller *user.User, owner, repo string) (json.RawMessage, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.Template(ctx, owner, repo)
}

// Icon is a release's icon, fetched through the instance so the
// dashboard's browser never talks to the catalog.
func (s *Service) Icon(ctx context.Context, caller *user.User, repository, file string) ([]byte, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	return c.Icon(ctx, repository, file)
}

// Get is one install and how it is going.
func (s *Service) Get(ctx context.Context, caller *user.User, id int64) (*Install, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	return s.records.ByID(ctx, id)
}

// List is the most recent installs.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Install, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	return s.records.List(ctx, 50)
}

// Request is what an install is asked to do.
type Request struct {
	Owner string
	Repo  string
	// Release is a tag the catalog accepted. Empty is the newest one.
	Release string
	// Project and Environment are where the apps go. Empty takes the
	// template's suggestion; either is created when it does not exist.
	Project     string
	Environment string
	// Databases, Stores and Apps rename what the template declares, by
	// key. A key left out keeps the template's name.
	Databases map[string]string
	Stores    map[string]string
	Apps      map[string]string
	// Inputs answers the template's questions, by key.
	Inputs map[string]string
}

// Install checks the request against the template and the instance,
// records the install and starts it. Nothing is created before every
// check has passed.
//
// The secrets the instance generated come back here and nowhere else:
// they are not recorded, only written into the apps that use them.
func (s *Service) Install(ctx context.Context, caller *user.User, req Request) (*Install, map[string]string, error) {
	if err := user.Require(caller, RoleToInstall); err != nil {
		return nil, nil, err
	}
	p, err := s.prepare(ctx, caller, req)
	if err != nil {
		return nil, nil, err
	}
	rec, err := s.records.Create(ctx, &Install{
		Owner: p.owner, Repo: p.repo, Release: p.release.Tag, Commit: p.release.Commit,
		Project: p.project, Environment: p.environment,
		Status: StatusRunning, Step: "Starting", CreatedBy: caller.ID,
	})
	if err != nil {
		return nil, nil, err
	}
	started := *rec
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(caller, rec, p)
	}()
	return &started, p.generated, nil
}

// plan is a request checked against the template and the instance, with
// every name and answer settled.
type plan struct {
	owner, repo string
	release     CatalogRelease
	manifest    *template.Normalized

	project, environment string
	createProject        bool
	createEnvironment    bool

	// databases, stores and apps are each template key's name on the
	// instance.
	databases map[string]string
	stores    map[string]string
	apps      map[string]string
	// storeInputs is each store input's answer: a store that already exists.
	storeInputs map[string]string
	inputs      map[string]string
	generated   map[string]string
}

func (s *Service) prepare(ctx context.Context, caller *user.User, req Request) (*plan, error) {
	c, err := s.source()
	if err != nil {
		return nil, err
	}
	releases, err := c.Releases(ctx, req.Owner, req.Repo)
	if err != nil {
		return nil, err
	}
	var chosen *CatalogRelease
	for i := range releases {
		if releases[i].Status == "accepted" && (req.Release == "" || releases[i].Tag == req.Release) {
			chosen = &releases[i]
			break
		}
	}
	if chosen == nil {
		return nil, ErrReleaseNotFound
	}
	source, err := c.Source(ctx, req.Owner, req.Repo, chosen.Commit)
	if err != nil {
		return nil, err
	}
	result := template.Validate(source)
	if !result.OK {
		var refused []template.Diagnostic
		for _, d := range result.Diagnostics {
			if d.Severity == template.Error {
				refused = append(refused, d)
			}
		}
		return nil, &InvalidTemplateError{Diagnostics: refused}
	}
	m := result.Manifest
	if m.MinCubeship != nil {
		ok, err := template.Satisfies(*m.MinCubeship, s.version)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: it asks for %s and this instance runs %s", ErrTooNew, *m.MinCubeship, s.version)
		}
	}

	p := &plan{
		owner: req.Owner, repo: req.Repo, release: *chosen, manifest: m,
		project:     or(req.Project, m.Project),
		environment: or(req.Environment, m.Environment),
		databases:   map[string]string{}, stores: map[string]string{}, apps: map[string]string{},
		storeInputs: map[string]string{}, inputs: map[string]string{}, generated: map[string]string{},
	}
	if err := checkSlug("project", p.project); err != nil {
		return nil, err
	}
	if err := checkSlug("environment", p.environment); err != nil {
		return nil, err
	}
	if _, err := s.projects.Resolve(ctx, caller, p.project, RoleToInstall); errors.Is(err, project.ErrNotFound) {
		p.createProject = true
		p.createEnvironment = p.environment != project.ProductionEnvSlug
	} else if err != nil {
		return nil, err
	} else if _, err := s.projects.ResolveEnvironment(ctx, caller, p.project, p.environment, RoleToInstall); errors.Is(err, project.ErrEnvironmentNotFound) {
		p.createEnvironment = true
	} else if err != nil {
		return nil, err
	}

	if err := s.nameDatabases(ctx, caller, p, req.Databases); err != nil {
		return nil, err
	}
	if err := s.nameStores(ctx, caller, p, req.Stores); err != nil {
		return nil, err
	}
	if err := s.nameApps(ctx, caller, p, req.Apps); err != nil {
		return nil, err
	}
	if err := s.answer(ctx, caller, p, req.Inputs); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) nameDatabases(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, db := range p.manifest.Databases {
		key := "databases." + db.Key
		name := or(strings.TrimSpace(overrides[db.Key]), db.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two databases would both be called " + name}
		}
		used[name] = true
		if _, err := s.datastores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "database", Name: name}
		} else if !errors.Is(err, datastore.ErrNotFound) {
			return err
		}
		p.databases[db.Key] = name
	}
	return nil
}

func (s *Service) nameStores(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, st := range p.manifest.Stores {
		key := "stores." + st.Key
		name := or(strings.TrimSpace(overrides[st.Key]), st.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two object stores would both be called " + name}
		}
		used[name] = true
		if _, err := s.stores.Resolve(ctx, caller, name, RoleToInstall); err == nil {
			return &TakenError{Kind: "object store", Name: name}
		} else if !errors.Is(err, objectstore.ErrNotFound) {
			return err
		}
		p.stores[st.Key] = name
	}
	return nil
}

func (s *Service) nameApps(ctx context.Context, caller *user.User, p *plan, overrides map[string]string) error {
	used := map[string]bool{}
	for _, a := range p.manifest.Apps {
		key := "apps." + a.Key
		name := or(strings.TrimSpace(overrides[a.Key]), a.Name)
		if err := checkSlug(key, name); err != nil {
			return err
		}
		if used[name] {
			return &InputError{Key: key, Message: "two apps would both be called " + name}
		}
		used[name] = true
		// A project or environment about to be created has no apps.
		if !p.createProject && !p.createEnvironment {
			ref := app.Reference{Project: p.project, Environment: p.environment, Name: name}
			if _, err := s.apps.Resolve(ctx, caller, ref, RoleToInstall); err == nil {
				return &TakenError{Kind: "app", Name: ref.String()}
			} else if !errors.Is(err, app.ErrNotFound) {
				return err
			}
		}
		p.apps[a.Key] = name
	}
	return nil
}

// answer settles every input: the caller's answer, else the template's
// default, else a generated secret — and checks each against its type.
func (s *Service) answer(ctx context.Context, caller *user.User, p *plan, given map[string]string) error {
	hosts := map[string]bool{}
	for _, in := range p.manifest.Inputs {
		key := "inputs." + in.Key
		value := given[in.Key]
		if in.Type != "secret" {
			value = strings.TrimSpace(value)
		}
		if value == "" && in.Default != nil {
			value = fmt.Sprint(in.Default)
		}
		if value == "" && in.Type == "secret" && in.Generate != nil {
			generated, err := generateSecret(*in.Generate)
			if err != nil {
				return err
			}
			value = generated
			p.generated[in.Key] = generated
		}
		if value == "" {
			if in.Required {
				return &InputError{Key: key, Message: in.Label + " is required"}
			}
			continue
		}

		switch in.Type {
		case "domain":
			host := app.NormalizeHost(value)
			if !app.ValidHost(host) {
				return &InputError{Key: key, Message: value + " is not a hostname"}
			}
			if hosts[host] {
				return &InputError{Key: key, Message: host + " is the answer to another domain as well"}
			}
			hosts[host] = true
			taken, err := s.apps.HostTaken(ctx, host)
			if err != nil {
				return err
			}
			if taken {
				return &TakenError{Kind: "domain", Name: host}
			}
			value = host
		case "number":
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return &InputError{Key: key, Message: value + " is not a number"}
			}
			if in.Min != nil && n < *in.Min {
				return &InputError{Key: key, Message: fmt.Sprintf("is at least %v", *in.Min)}
			}
			if in.Max != nil && n > *in.Max {
				return &InputError{Key: key, Message: fmt.Sprintf("is at most %v", *in.Max)}
			}
		case "choice":
			if !slices.Contains(in.Options, value) {
				return &InputError{Key: key, Message: "is one of " + strings.Join(in.Options, ", ")}
			}
		case "text":
			if in.Pattern != "" {
				// A template's pattern matches the whole answer, the way an
				// HTML pattern attribute does.
				re, err := regexp.Compile("^(?:" + in.Pattern + ")$")
				if err != nil {
					return &InputError{Key: key, Message: "the template's pattern cannot be read here: " + err.Error()}
				}
				if !re.MatchString(value) {
					return &InputError{Key: key, Message: "does not match " + in.Pattern}
				}
			}
		case "store":
			if _, err := s.stores.Resolve(ctx, caller, value, RoleToInstall); errors.Is(err, objectstore.ErrNotFound) {
				return &InputError{Key: key, Message: "there is no object store called " + value}
			} else if err != nil {
				return err
			}
			p.storeInputs[in.Key] = value
		}
		p.inputs[in.Key] = value
	}
	return nil
}

func checkSlug(key, value string) error {
	if slug.Reserved(value) {
		return &InputError{Key: key, Message: slug.ErrReserved.Error()}
	}
	if !slug.Valid(value) {
		return &InputError{Key: key, Message: value + " " + slug.ErrInvalid.Error()}
	}
	return nil
}

const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// generateSecret is n letters and digits: long enough is the whole of its
// strength, and nothing that reads it has to quote a symbol.
func generateSecret(n int) (string, error) {
	out := make([]byte, n)
	limit := big.NewInt(int64(len(secretAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		out[i] = secretAlphabet[v.Int64()]
	}
	return string(out), nil
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
