package datastore

import (
	"context"
	"fmt"
	"io"
	"log"
	"maps"

	"cubeship/internal/envvar"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database"
	"cubeship/internal/user"
)

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
// parts. It is needed when two attachments would name the same
// variables — two SQL databases, both writing DATABASE_URL — and not
// when they would not: a Redis writes REDIS_URL and a MongoDB
// MONGO_URL, so either sits beside a Postgres at the same prefix
// without a word.
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

	if err := s.Repo().Attach(ctx, d.ID, a.ID, prefix, d.Engine.VarStem()); err != nil {
		switch {
		case database.UniqueViolationOn(err, "datastore_attachments_pair"):
			return nil, ErrAlreadyAttached
		case database.UniqueViolationOn(err, "datastore_attachments_app_vars"):
			// Which variables, not which prefix: the app may well have
			// two databases attached already and be colliding with
			// exactly one of them.
			return nil, PrefixTakenError(prefix, d.Engine.VarStem())
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
	if d.ExposedPort != 0 {
		s.syncPorts(ctx)
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

// --- what backups need, and nothing more ---

// ByID and BySlug answer which database, with no caller.
//
// **They authorize nothing, deliberately.** `internal/backup` has
// already settled whether somebody may back this database up before it
// asks which one — and the scheduler has no caller to ask about at all.
// Same shape `metrics.Service` has: asking twice is two answers to a
// question with one.
func (s *Service) ByID(ctx context.Context, id int64) (*Datastore, error) {
	return s.Repo().ByID(ctx, id)
}

func (s *Service) BySlug(ctx context.Context, slug string) (*Datastore, error) {
	return s.Repo().BySlug(ctx, slug)
}

// All is every database on the instance, for the coverage report.
//
// It reads the rows and nothing else — no attachments, no containers,
// which List assembles for the screen that shows them. What backups
// need is which databases exist, and the one question the report is for
// is about the ones with no backup at all: a database that has never
// been dumped appears in no list of dumps.
func (s *Service) All(ctx context.Context) ([]*Datastore, error) {
	return s.Repo().List(ctx)
}

// Exec runs a command inside a database's container, streaming stdin in
// and stdout out.
//
// It is here rather than in `backup` because the container is this
// module's: which one a datastore is running, and whether it is running
// at all, is a fact it owns. What the command *is* comes from the
// engine's spec, which is also here; what it is *for* is the caller's.
func (s *Service) Exec(ctx context.Context, d *Datastore, cmd, env []string, in io.Reader, out io.Writer) (string, error) {
	if d.ContainerID == "" {
		return "", ErrNotRunning
	}
	running, err := s.prov.docker.IsRunning(ctx, d.ContainerID)
	if err != nil || !running {
		// A dump against a stopped database is not a smaller dump, it
		// is a connection refused several seconds later — said here
		// instead, where it is the reason rather than the symptom.
		return "", ErrNotRunning
	}

	// The engine's own environment is carried alongside the command's:
	// `docker exec` starts a process with the container's environment,
	// and these are the two or three variables the tools read for a
	// password.
	full := append([]string{}, cmd...)
	stderr, code, err := s.prov.docker.ExecStream(ctx, d.ContainerID, withEnv(full, env), in, out)
	if err != nil {
		return stderr, err
	}
	if code != 0 {
		return stderr, fmt.Errorf("%s exited with status %d", cmd[0], code)
	}
	return stderr, nil
}

// withEnv wraps a command so it runs with extra variables set.
//
// `env` is a POSIX utility rather than a shell builtin, so this is argv
// and never a string: nothing here is parsed, quoted or split, and a
// password containing a space, a quote or a semicolon reaches the tool
// exactly as it is stored. A `sh -c` with the same values interpolated
// would be a shell reading somebody's password as syntax.
func withEnv(cmd, env []string) []string {
	if len(env) == 0 {
		return cmd
	}
	out := append([]string{"env"}, env...)
	return append(out, cmd...)
}
