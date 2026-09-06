package datastore

import (
	"context"
	"fmt"
	"io"
	"log"
	"maps"

	"cubeship/internal/app"
	"cubeship/internal/envvar"
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
const RoleToManage = user.RoleAdmin

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
		out = append(out, metrics.Subject{Kind: metrics.KindDatastore, ID: d.ID, ContainerID: d.ContainerID})
	}
	return out, nil
}

// Series is a database's recent CPU and memory.
//
// A member's, like every other read here: what a database is using is
// part of knowing whether the app in front of it is slow because of
// it, and that is not an admin's question.
func (s *Service) Series(ctx context.Context, caller *user.User, name, window string) (metrics.Series, error) {
	d, err := s.Resolve(ctx, caller, name, user.RoleMember)
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
func (s *Service) Resolve(ctx context.Context, caller *user.User, name string, minRole user.Role) (*Datastore, error) {
	if err := user.Require(caller, minRole); err != nil {
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
	if err := user.Require(caller, RoleToManage); err != nil {
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
		var err error
		if port, err = s.resolvePort(ctx, *spec.Expose); err != nil {
			return nil, err
		}
	}

	created, err := s.Repo().Create(ctx, &Datastore{
		Slug: spec.Slug, Description: spec.Description,
		Engine: spec.Engine, Version: spec.Version,
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
func (s *Service) Update(ctx context.Context, caller *user.User, name string, description *string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if _, err := s.Repo().Update(ctx, d.ID, description); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// List is every database on the instance, each with what it is attached
// to. The attachments come back in one query rather than one per row —
// this is the screen the sidebar opens on.
func (s *Service) List(ctx context.Context, caller *user.User) ([]*Datastore, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return nil, err
	}
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	byDatastore, err := s.Repo().AllAttachments(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range all {
		d.Attachments = byDatastore[d.ID]
	}
	return all, nil
}

// Credentials is the login for one datastore, and where to use it.
type Credentials struct {
	Username     string
	Password     string
	Database     string
	Internal     string
	InternalHost string
	InternalPort int
	// External is the connection string from off this host, present
	// only while the datastore is exposed and the instance has a domain
	// to be reached at.
	External     string
	ExternalHost string
	ExternalPort int
}

// Credentials reads the login. An admin's, and its own request rather
// than a field on the datastore: everything else about a database is
// worth listing on a screen, and this is worth asking for.
func (s *Service) Credentials(ctx context.Context, caller *user.User, name string) (Credentials, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return Credentials{}, err
	}
	host := ContainerName(d.Slug)
	creds := Credentials{
		Username: d.Username, Password: d.Password, Database: d.Database,
		Internal:     d.URI(host, d.Engine.Port()),
		InternalHost: host, InternalPort: d.Engine.Port(),
	}
	if d.ExposedPort != 0 {
		if external := s.externalHost(ctx); external != "" {
			creds.External = d.URI(external, d.ExposedPort)
			creds.ExternalHost = external
			creds.ExternalPort = d.ExposedPort
		}
	}
	return creds, nil
}

// externalHost is the name someone off this host connects to, which is
// the instance's own domain. Empty before there is one — an install
// reached by IP has no name to put in a connection string, and guessing
// the interface's address would be wrong on every host behind NAT.
func (s *Service) externalHost(ctx context.Context) string {
	values, err := s.settings.Load(ctx)
	if err != nil {
		return ""
	}
	return values.Get(settings.Domain)
}

// Expose publishes the datastore on a host port, so something that is
// not an app on this instance can connect: a migration run from a
// laptop, a BI tool, psql.
//
// port 0 means "pick one" — see PortRangeStart. Naming one is for the
// operator who has a firewall rule already written.
//
// It is off by default and worth leaving off. There is no TLS in front
// of this: Traefik terminates HTTPS for HTTP, and a database speaks its
// own protocol on its own port, so an exposed datastore is a password
// on the open internet. The two things that make it safe are the
// password — generated, and long — and a firewall. Both are the
// operator's, which is why this is a deliberate act with its own
// endpoint rather than a checkbox on the create form.
//
// The container is replaced to pick the port up, because a container's
// published ports are fixed when it is created. The data is a host bind
// mount, so it survives that untouched — the same property everything
// else in Cubeship relies on when a container's configuration changes.
func (s *Service) Expose(ctx context.Context, caller *user.User, name string, port int) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	chosen, err := s.resolvePort(ctx, port)
	if err != nil {
		return nil, err
	}
	if chosen == d.ExposedPort {
		return d, nil
	}
	return s.setPort(ctx, caller, d, chosen)
}

// Unexpose takes the datastore off its host port, leaving it reachable
// only by its neighbours on the shared network.
func (s *Service) Unexpose(ctx context.Context, caller *user.User, name string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if d.ExposedPort == 0 {
		return d, nil
	}
	return s.setPort(ctx, caller, d, 0)
}

func (s *Service) setPort(ctx context.Context, caller *user.User, d *Datastore, port int) (*Datastore, error) {
	if err := s.Repo().SetExposedPort(ctx, d.ID, port); err != nil {
		if database.UniqueViolationOn(err, portIndex) {
			return nil, ErrPortTaken
		}
		return nil, err
	}
	updated, err := s.Resolve(ctx, caller, d.Slug, RoleToManage)
	if err != nil {
		return nil, err
	}
	// Back to provisioning, and detached: the container is replaced,
	// which is a pull-free create-and-start but still not something to
	// hold a request open for. The row is where the outcome goes.
	if err := s.Repo().UpdateContainer(ctx, updated.ID, updated.ContainerID, StatusProvisioning, ""); err != nil {
		return nil, err
	}
	updated.Status = StatusProvisioning
	s.prov.Start(updated)
	return updated, nil
}

// resolvePort turns what a caller asked for into a port to publish on.
// 0 means "pick one".
func (s *Service) resolvePort(ctx context.Context, want int) (int, error) {
	used, err := s.Repo().UsedPorts(ctx)
	if err != nil {
		return 0, err
	}
	if want != 0 {
		// Below 1024 needs privileges this container does not have, and
		// the low numbers are where everything else on a host already
		// is. Beyond that Docker is the authority — a port held by
		// something Cubeship did not start fails at bind time, and no
		// list here could have known about it.
		if want < 1024 || want > 65535 {
			return 0, ErrBadPort
		}
		if used[want] {
			return 0, ErrPortTaken
		}
		return want, nil
	}
	for port := PortRangeStart; port <= PortRangeEnd; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, ErrNoPortsLeft
}

// DefaultLogTail is how much of a database's log the API returns when
// the caller does not ask for a specific amount. An engine that has
// been up for weeks holds far more than anyone wants streamed at them,
// and the recent lines are the ones that explain what is happening now.
const DefaultLogTail = "500"

// Logs is what the engine has written.
//
// A member's, like every other read here: the reason a database is
// refusing connections is in its log, and finding that out is not an
// admin's privilege. It carries no credential — the engine prints its
// own startup, not what Cubeship configured it with.
func (s *Service) Logs(ctx context.Context, caller *user.User, name, tail string) (io.ReadCloser, error) {
	d, err := s.Resolve(ctx, caller, name, user.RoleMember)
	if err != nil {
		return nil, err
	}
	if d.ContainerID == "" {
		return nil, ErrNotRunning
	}
	if tail == "" {
		tail = DefaultLogTail
	}
	return s.prov.Logs(ctx, d, tail)
}

// Stop turns a database off, leaving its container and its data where
// they are.
//
// An admin's, and not a small act: every app attached to this keeps
// running and starts failing to connect. That is sometimes exactly what
// you want — a migration nobody should be writing through, an engine
// eating the box — which is why it exists, and why the screen that
// offers it names what is attached.
//
// The container is stopped rather than removed, so its log survives:
// what somebody wants immediately after turning a database off is
// usually the reason they turned it off.
func (s *Service) Stop(ctx context.Context, caller *user.User, name string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if d.ContainerID == "" {
		return nil, ErrNotRunning
	}
	if err := s.prov.Stop(ctx, d); err != nil {
		return nil, err
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// Start brings a stopped database back.
//
// It provisions rather than starting the container that is already
// there, and that is one path instead of two: the container is
// recreated from the same options, and the data is a host bind mount
// that a recreate does not touch. The alternative — start the existing
// one, unless there isn't one, in which case provision — is two code
// paths where one of them is exercised and the other is not.
func (s *Service) Start(ctx context.Context, caller *user.User, name string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if err := s.Repo().UpdateContainer(ctx, d.ID, d.ContainerID, StatusProvisioning, ""); err != nil {
		return nil, err
	}
	d.Status = StatusProvisioning
	s.prov.Start(d)
	return d, nil
}

// Attach wires an app to this datastore: the app's container is given
// the connection variables from its next deploy onwards.
//
// From its next deploy, not now. A container keeps the environment it
// was created with — the same rule that makes adding a domain take
// effect on redeploy — so attaching something an app is already running
// against changes nothing until it is deployed again.
//
// appRef is the app's full reference, project/environment/name. It has
// to be: a datastore is not inside an environment, so a bare name would
// identify nothing, and one database serving apps in two projects is
// the reason this module is instance-wide at all.
//
// prefix is empty for the usual case and gives DATABASE_URL and its
// parts. An app that needs two databases names one of them, because two
// under the same prefix would be one variable with two values.
func (s *Service) Attach(ctx context.Context, caller *user.User, name, appRef, prefix string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if err := CheckPrefix(prefix); err != nil {
		return nil, err
	}
	a, err := s.apps.ResolveString(ctx, caller, appRef, RoleToManage)
	if err != nil {
		return nil, err
	}

	if err := s.Repo().Attach(ctx, d.ID, a.ID, prefix); err != nil {
		switch {
		case database.UniqueViolationOn(err, "datastore_attachments_pair"):
			return nil, ErrAlreadyAttached
		case database.UniqueViolationOn(err, "datastore_attachments_app_prefix"):
			return nil, ErrPrefixTaken
		case database.IsUniqueViolation(err):
			return nil, ErrAlreadyAttached
		}
		return nil, fmt.Errorf("attach datastore: %w", err)
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// Detach unwires an app. Its container keeps the variables it was
// created with until it is deployed again — which is worth knowing,
// because detaching is not how you cut an app off from a database in a
// hurry.
func (s *Service) Detach(ctx context.Context, caller *user.User, name, appRef string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	a, err := s.apps.ResolveString(ctx, caller, appRef, RoleToManage)
	if err != nil {
		return nil, err
	}
	removed, err := s.Repo().Detach(ctx, d.ID, a.ID)
	if err != nil {
		return nil, err
	}
	if !removed {
		return nil, ErrNotAttached
	}
	return s.Resolve(ctx, caller, name, RoleToManage)
}

// VarsForApp is what app asks this module for: the variables every
// database attached to one app contributes to its container.
//
// The host is the datastore's container name, which Docker resolves on
// the shared network — that is the whole of the internal wiring, and
// the reason an app never has to be told an address.
func (s *Service) VarsForApp(ctx context.Context, appID int64) (envvar.Map, error) {
	attached, err := s.Repo().AttachedTo(ctx, appID)
	if err != nil {
		return nil, err
	}
	vars := envvar.Map{}
	for i := range attached {
		a := &attached[i]
		maps.Copy(vars, a.Vars(a.Prefix, ContainerName(a.Slug), a.Engine.Port()))
	}
	return vars, nil
}

// Delete removes a datastore: its container, its data, then its row.
//
// The data goes. A "managed database" whose files outlived it would
// leave a directory nothing on the instance names any more and nothing
// ever reclaims — the same shape as the registry images and the build
// cache that already need a pass Cubeship does not run, except that
// this one is the largest thing on the disk. The guard is the
// confirmation in front of it, which asks for the datastore's own name.
//
// Its attachments go with the row, and the apps that had them keep
// running: a container holds the environment it was created with, so
// nothing breaks until they are deployed again — at which point they
// will come up without a DATABASE_URL. That is worth saying out loud
// wherever this is offered.
//
// Container and files first, outside any transaction, because neither
// Docker nor the filesystem has a rollback. A failure there leaves the
// row standing, which a retry finishes; the reverse would leave a
// database running with nothing on the instance naming it.
func (s *Service) Delete(ctx context.Context, caller *user.User, name string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	if err := s.prov.Teardown(ctx, d, false); err != nil {
		return nil, err
	}
	if err := s.Repo().Delete(ctx, d.ID); err != nil {
		return nil, err
	}
	// Its history goes with it, so a later datastore reusing this id
	// does not inherit a chart of a stranger's.
	return d, s.metrics.Forget(ctx, metrics.KindDatastore, d.ID)
}

// Reconcile corrects each datastore's recorded status against what
// Docker is actually running. It runs at startup, when the database may
// describe a world from before a reboot.
//
// Like the app reconciler it never starts, stops or removes anything:
// correcting the record is safe, and acting on a stale one is how a
// reconciler takes down a working database.
func Reconcile(ctx context.Context, repo *Repository, d interface {
	IsRunning(ctx context.Context, id string) (bool, error)
}) error {
	all, err := repo.List(ctx)
	if err != nil {
		return err
	}
	for _, ds := range all {
		if ds.ContainerID == "" {
			continue
		}
		running, err := d.IsRunning(ctx, ds.ContainerID)
		if err != nil {
			log.Printf("reconcile: datastore %s: inspect container %s failed: %v",
				ds.Slug, ds.ContainerID, err)
			running = false
		}
		want := StatusDown
		if running {
			want = StatusRunning
		}
		// A datastore that failed to provision keeps saying so. There
		// is no container to have gone away, and "down" would lose the
		// only explanation anybody has.
		//
		// A stopped one keeps saying so too, and for a better reason:
		// somebody turned it off, and Docker's unless-stopped policy
		// means it is still off on purpose after a reboot. Rewriting
		// that to "down" would turn a decision into what looks like a
		// fault, every time the daemon restarts.
		if !running && (ds.Status == StatusFailed || ds.Status == StatusStopped) {
			continue
		}
		if want != ds.Status {
			log.Printf("reconcile: datastore %s: status %s -> %s", ds.Slug, ds.Status, want)
			if err := repo.UpdateContainer(ctx, ds.ID, ds.ContainerID, want, ds.Error); err != nil {
				return err
			}
		}
	}
	return nil
}
