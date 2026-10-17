package datastore

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"

	"cubeship/internal/app"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/slug"
	"cubeship/internal/user"
)

// Index names the service tells one "already exists" from another. They
// are the names the migrations give them.
const (
	slugIndex = "datastores_slug"
	portIndex = "datastores_exposed_port"
)

// RoleToManage is the role every write here takes.
//
// An admin's, all of it. Creating a database starts a container and
// claims disk on this host that nothing reclaims on its own; attaching
// one hands its password to an app. Neither is the kind of act a member
// does — a member deploys images somebody else already published.
//
// Reading is a member's, minus the credentials: seeing what databases
// the instance runs, and which apps use them, is part of understanding
// what you are deploying into.
const RoleToManage = user.LevelManage

// Service holds the datastore use cases.
//
// It sits above app, which is what lets it resolve the app an
// attachment names. The dependency runs the other way for the one thing
// app needs back — what an attached database contributes to a
// container's environment — which app asks for through an interface
// this satisfies. See app.DatastoreVars.
//
// There is no project dependency at all any more: a datastore is the
// instance's, and an attachment reaches an app by its own full
// reference.
type Service struct {
	db       *database.DB
	apps     *app.Service
	prov     *Provisioner
	settings *settings.Service
	// metrics answers what this database's container has been using.
	// The same package the app module uses: a database and an app are
	// both a container with a CPU and a resident set.
	metrics *metrics.Service
	// portsChanged is told after anything changes what this module
	// publishes on the host. Nil in tests and wherever nothing is set.
	portsChanged PortsChanged
}

// PortsChanged is told whenever what this module publishes on the host changes.
type PortsChanged interface {
	SyncPublished(ctx context.Context) error
}

// SetPortsChanged names what is told when an exposed port comes or goes.
// A setter because what satisfies it is built after this module.
func (s *Service) SetPortsChanged(p PortsChanged) { s.portsChanged = p }

// syncPorts tells it, best effort: the expose has happened either way,
// and refusing the request would not undo it.
func (s *Service) syncPorts(ctx context.Context) {
	if s.portsChanged == nil {
		return
	}
	if err := s.portsChanged.SyncPublished(ctx); err != nil {
		log.Printf("datastore: could not bring the host firewall in line with what is exposed: %v", err)
	}
}

func NewService(db *database.DB, apps *app.Service, prov *Provisioner,
	cfg *settings.Service, series *metrics.Service) *Service {
	return &Service{db: db, apps: apps, prov: prov, settings: cfg, metrics: series}
}

// Metrics exposes the series service to this module's own handlers.
func (s *Service) Metrics() *metrics.Service { return s.metrics }

// MetricSubjects is what the collector samples on this module's behalf:
// every datastore with a container behind it right now.
func (s *Service) MetricSubjects(ctx context.Context) ([]metrics.Subject, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]metrics.Subject, 0, len(all))
	for _, d := range all {
		if d.ContainerID == "" {
			continue
		}
		out = append(out, metrics.Subject{
			Kind: metrics.KindDatastore, ID: d.ID, ContainerID: d.ContainerID, Name: d.Slug,
		})
	}
	return out, nil
}

// Series is a database's recent CPU and memory.
//
// A member's, like every other read here: what a database is using is
// part of knowing whether the app in front of it is slow because of
// it, and that is not an admin's question.
func (s *Service) Series(ctx context.Context, caller *user.User, name, window string) (metrics.Series, error) {
	d, err := s.Resolve(ctx, caller, name, user.LevelView)
	if err != nil {
		return metrics.Series{}, err
	}
	return s.metrics.Series(ctx, metrics.KindDatastore, d.ID, window, d.ContainerID != "")
}

func (s *Service) Repo() *Repository         { return NewRepository(s.db) }
func (s *Service) Provisioner() *Provisioner { return s.prov }
func (s *Service) WaitForProvisioning()      { s.prov.Wait() }

// Resolve looks up a datastore by name and requires minRole of the
// caller, loading the apps attached to it — with nothing above a
// datastore, what it is wired to is the whole of where it sits.
func (s *Service) Resolve(ctx context.Context, caller *user.User, name string, need user.Level) (*Datastore, error) {
	if err := user.Allow(caller, user.ResDatabases, need, name); err != nil {
		if errors.Is(err, user.ErrHidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	attachments, err := s.Repo().Attachments(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	d.Attachments = attachments
	return d, nil
}

// Spec is what a datastore is created from. Everything but the name has
// an answer Cubeship supplies when the caller does not.
type Spec struct {
	Slug        string
	Description string
	Engine      Engine
	// Version is a tag this release offers for that engine. Empty takes
	// the newest.
	Version string
	// Extensions are the Postgres extensions to create the database
	// with, by their public names — "pgvector", "vectorchord". Empty is
	// the normal answer, and it keeps the plain engine image.
	//
	// Names, not images or SQL: what each one runs is in the support
	// matrix, and anything not in it is refused. Permanent, so this is
	// only ever read at creation.
	Extensions []string
	// Username and Database default to something workable; Password is
	// generated when it is empty, so a database without a strong
	// password is not something anybody can create by leaving a box
	// alone.
	Username string
	Password string
	Database string
	// Expose asks for a host port at creation. Nil is the normal answer
	// — see Service.Expose for what saying yes means.
	Expose *int
}

// Create provisions a database on this instance.
//
// The row is written and the container is started detached, so this
// returns as soon as there is something to report on rather than
// holding the request open for an image pull. The datastore comes back
// in "provisioning"; how it went lands on the same row.
func (s *Service) Create(ctx context.Context, caller *user.User, spec Spec) (*Datastore, error) {
	if err := user.Allow(caller, user.ResDatabases, RoleToManage, spec.Slug); err != nil {
		return nil, err
	}
	// The name is the container's, which is the host every attached app
	// resolves, so it is checked before anything else happens.
	if slug.Reserved(spec.Slug) {
		return nil, slug.ErrReserved
	}
	if reservedSlugs[spec.Slug] {
		return nil, ErrReservedSlug
	}
	if !slug.Valid(spec.Slug) {
		return nil, slug.ErrInvalid
	}
	if !spec.Engine.Valid() {
		return nil, fmt.Errorf("%w: %q — this release runs %s", ErrUnknownEngine, spec.Engine, engineList())
	}
	if spec.Version == "" {
		spec.Version = spec.Engine.DefaultVersion()
	}
	if !spec.Engine.KnowsVersion(spec.Version) {
		return nil, fmt.Errorf("%w: %s %q — this release offers %v",
			ErrUnknownVersion, spec.Engine, spec.Version, spec.Engine.Versions())
	}
	// After the version, because which extensions exist is a fact about
	// a version: the answer for "vectorchord on postgres 16" is not the
	// answer for a version nobody has an image of it for. Here rather
	// than in a handler, so the MCP surface and a template install are
	// held to exactly the same list.
	extensions, err := NormalizeExtensions(spec.Engine, spec.Version, spec.Extensions)
	if err != nil {
		return nil, err
	}
	if spec.Username == "" {
		// Per engine, because Redis has exactly one login and it is
		// not ours to name.
		spec.Username = spec.Engine.DefaultUsername()
	}
	if err := CheckUsername(spec.Engine, spec.Username); err != nil {
		return nil, err
	}
	if spec.Password == "" {
		generated, err := GeneratePassword()
		if err != nil {
			return nil, err
		}
		spec.Password = generated
	}
	if !spec.Engine.HasDatabase() {
		// An engine with no named databases is handed nothing to
		// create, rather than a name it would ignore.
		spec.Database = ""
	} else {
		if spec.Database == "" {
			spec.Database = DefaultDatabaseName(spec.Slug)
		}
		if err := CheckDatabaseName(spec.Database); err != nil {
			return nil, err
		}
	}

	port := 0
	if spec.Expose != nil {
		if port, err = s.resolvePort(ctx, *spec.Expose); err != nil {
			return nil, err
		}
	}

	created, err := s.Repo().Create(ctx, &Datastore{
		Slug: spec.Slug, Description: spec.Description,
		Engine: spec.Engine, Version: spec.Version, Extensions: extensions,
		Username: spec.Username, Password: spec.Password, Database: spec.Database,
		ExposedPort: port,
	})
	if err != nil {
		// The unique index is the authority, not a preceding lookup:
		// two concurrent creates would both pass a check and the loser
		// would surface as a 500. Which index it was decides which of
		// two sentences the caller gets.
		if database.UniqueViolationOn(err, portIndex) {
			return nil, ErrPortTaken
		}
		if database.UniqueViolationOn(err, slugIndex) || database.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, err
	}

	s.prov.Start(created)
	if created.ExposedPort != 0 {
		s.syncPorts(ctx)
	}
	return created, nil
}

func engineList() string {
	out := ""
	for i, e := range Engines() {
		if i > 0 {
			out += ", "
		}
		out += string(e)
	}
	return out
}

// Update changes a datastore's description, and only that.
//
// Not the name, for the reason no identifier in Cubeship is editable:
// it is the container's own name, which every attached app resolves on
// the shared network, and renaming it would silently point them at a
// host that stopped existing.
//
// Not the engine or the version either, and those are worse. A data
// directory is written by one major version of one engine and is not
// readable by another; a datastore that "changed version" would be a
// container that will not start, with the only copy of the data inside
// the directory it will not read. Running a new version means creating
// a new datastore and moving the data with the engine's own tools,
// which is a decision with a plan behind it rather than a dropdown.
//
// Not the password, which is subtler: it is used once, when the engine
// initializes itself, and nothing reads it afterwards. Changing this
// column would change every connection string Cubeship hands out while
// the database went on accepting only the old one.
//
// Not the extensions, which are not fixed but are not a field either:
// adding one replaces the container, and that does not belong under a
// PATCH whose other two fields are a sentence and a memory ceiling. See
// AddExtensions.
func (s *Service) Update(ctx context.Context, caller *user.User, name string,
	description *string, l *Limits, extensions *[]string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	// Refused rather than ignored, and refused here rather than by a
	// handler that simply has no field for it: a request that asks for
	// an extension and gets a 200 back is somebody believing their
	// database has one. Adding one replaces the container, which is not
	// what a PATCH of a description should do — AddExtensions is where
	// that is asked for deliberately. Sending exactly what is already
	// there is not a change, so it is not an error either.
	if extensions != nil {
		want, err := NormalizeExtensions(d.Engine, d.Version, *extensions)
		if err != nil {
			return nil, err
		}
		if !slices.Equal(want, d.Extensions) {
			return nil, ErrExtensionsNotHere
		}
	}
	// Checked here rather than found out by a container that will not
	// start, minutes later, with nobody watching.
	if l != nil && !l.Valid() {
		return nil, ErrInvalidLimits
	}
	updated, err := s.Repo().Update(ctx, d.ID, description, l)
	if err != nil {
		return nil, err
	}
	// A ceiling is the one part of a database's container that changes
	// without the container being replaced, which is the difference
	// between raising its memory and a database going away for a few
	// seconds.
	if l != nil {
		if err := s.prov.Cap(ctx, updated); err != nil {
			return nil, err
		}
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}
