package datastore

import (
	"context"
	"fmt"
	"io"
	"slices"

	"cubeship/internal/platform/database"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// AddExtensions installs extensions on a database that already exists.
//
// **Add only.** Every extension the datastore already has stays, and
// asking for fewer is refused rather than quietly ignored: adding one is
// an image carrying one more library over the same data directory, and
// removing one is an image *without* a library that a column's type, an
// index or a default may already need. The second is a database that
// comes up and cannot read its own tables, and nothing here can tell in
// advance which one it would be.
//
// **Most of them interrupt nothing.** A contrib module is already inside
// every image this instance runs, so adding one is a statement against
// the server that is up. Two things are not: an extension with an image
// of its own, and one whose library the postmaster has to preload. Both
// are fixed when a container is created, so both replace it — a database
// going away for a few seconds, its data untouched because the data is a
// host bind mount.
//
// A replacement is detached, like every other provision here: the row is
// written first, the status goes back to "provisioning", and how it went
// lands on the same row. That is also what makes it retryable — a
// failure leaves the row asking for the extensions, and `start`
// provisions again.
//
// A database that is stopped, or that never produced a container, is
// only written down: the next `start` creates them, on the path that
// creates them for every other datastore.
//
// Idempotent. Asking for what is already there replaces nothing and
// returns the datastore untouched, which is what makes this safe to call
// from a script that does not track state.
func (s *Service) AddExtensions(ctx context.Context, caller *user.User, name string, names []string) (*Datastore, error) {
	d, err := s.Resolve(ctx, caller, name, RoleToManage)
	if err != nil {
		return nil, err
	}
	// Normalized as one set, so what an extension requires is added even
	// when only the other one was asked for, and so the list is checked
	// against the version this database actually runs.
	want, err := NormalizeExtensions(d.Engine, d.Version, append(d.Extensions.Strings(), names...))
	if err != nil {
		return nil, err
	}
	for _, have := range d.Extensions {
		if !want.Has(have) {
			return nil, fmt.Errorf("%w: %s", ErrExtensionRemoval, have)
		}
	}
	if slices.Equal(want, d.Extensions) {
		return d, nil
	}

	after := *d
	after.Extensions = want
	replace := NeedsReplacement(d, &after)

	// Nothing to run against: a database that is stopped, or one whose
	// provisioning never produced a container. The list is stored and
	// the next `start` creates them, which is the same code path every
	// other provision takes.
	if d.ContainerID == "" || d.Status != StatusRunning {
		if err := s.Repo().SetExtensions(ctx, d.ID, want); err != nil {
			return nil, err
		}
		return s.Resolve(ctx, caller, name, RoleToManage)
	}

	if !replace {
		// The usual case, and the reason most of these cost nothing: a
		// contrib module is already in the image, so the whole of adding
		// it is a statement against the server that is running.
		//
		// Run **before** the row is written. A failure here leaves the
		// datastore saying exactly what it has, which is what stops a
		// refused statement from turning into a row that claims an
		// extension nobody created.
		if err := s.prov.Install(ctx, &after); err != nil {
			return nil, err
		}
		if err := s.Repo().SetExtensions(ctx, d.ID, want); err != nil {
			return nil, err
		}
		return s.Resolve(ctx, caller, name, RoleToManage)
	}

	// A new image or a new preloaded library: both are fixed when a
	// container is created, so the container is replaced. The row goes
	// first, because it is what chooses the image — and because a
	// provision that fails halfway then leaves the datastore asking for
	// the extensions, so `start` finishes it rather than reverting it.
	if err := s.Repo().SetExtensions(ctx, d.ID, want); err != nil {
		return nil, err
	}
	d.Extensions = want
	if err := s.Repo().UpdateContainer(ctx, d.ID, d.ContainerID, StatusProvisioning, ""); err != nil {
		return nil, err
	}
	d.Status = StatusProvisioning
	s.prov.Start(d)
	return d, nil
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
	seen := all[:0]
	for _, d := range all {
		if !user.Sees(caller, user.ResDatabases, d.Slug) {
			continue
		}
		d.Attachments = byDatastore[d.ID]
		seen = append(seen, d)
	}
	all = seen
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
	d, err := s.Resolve(ctx, caller, name, user.LevelView)
	if err != nil {
		return Credentials{}, err
	}
	if err := user.AllowSecrets(caller, user.ResDatabases, name); err != nil {
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
	// Asked for the port it already holds: nothing to change, and the
	// port is in use — by this one — so resolvePort would refuse it.
	if port != 0 && port == d.ExposedPort {
		return d, nil
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
	s.syncPorts(ctx)
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
	d, err := s.Resolve(ctx, caller, name, user.LevelView)
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
