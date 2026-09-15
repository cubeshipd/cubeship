package datastore

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"

	"github.com/docker/docker/pkg/stdcopy"
)

// DockerAPI is the subset of dockerx.Client the container lifecycle
// needs. *dockerx.Client satisfies it structurally, and a test supplies
// a fake.
//
// Deliberately the same set app's deploy engine takes, so the daemon
// hands one client to both and a test that fakes Docker fakes it once.
type DockerAPI interface {
	PullImage(ctx context.Context, ref string, creds *dockerx.RegistryAuth) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	SetResources(ctx context.Context, id string, r dockerx.Resources) error
	IsRunning(ctx context.Context, id string) (bool, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
	// ExecStream is what a backup rides on: a dump is written to stdout
	// and a restore read from stdin, both streamed rather than
	// collected — the largest database this instance could copy would
	// otherwise be whatever it has left of memory.
	ExecStream(ctx context.Context, id string, cmd []string, in io.Reader, out io.Writer) (string, int, error)
}

// ProvisionTimeout bounds a detached provision. It is nobody's request
// timeout — the caller stopped waiting long ago — it only stops a wedged
// pull from running forever.
const ProvisionTimeout = 15 * time.Minute

// Provisioner owns a datastore's container. It is the only thing in
// Cubeship that creates or removes one.
//
// It is much smaller than the app orchestrator, and the difference is
// the point: there is no new image to swap to, so there is nothing to
// keep serving while a replacement warms up. A database is provisioned
// once, and afterwards Docker's restart policy is what keeps it up.
type Provisioner struct {
	db     *database.DB
	docker DockerAPI
	// dataDir is the instance's state directory *on the host*. The
	// daemon hands these paths to the Engine, which resolves them on the
	// host — which is exactly why the data directory is mounted at the
	// same path inside the daemon's own container and outside it.
	dataDir string

	// locks serializes work per datastore, so an expose racing a
	// provision cannot leave two containers under one name.
	locks sync.Map

	// running tracks provisions that outlive the request that started
	// them. Tests wait on it; the daemon does not.
	running sync.WaitGroup

	// ExtensionAttempts and ExtensionInterval bound how long a provision
	// waits for Postgres to start accepting connections before it
	// creates the extensions.
	//
	// Longer than ReadyAttempts, and separately so: that one is watching
	// for a container that dies, and this one is waiting for initdb,
	// which on a cold first start is the slowest thing a datastore does.
	ExtensionAttempts int
	ExtensionInterval time.Duration

	// ReadyAttempts and ReadyInterval bound how long a provision watches
	// a started container before calling it up. An engine that dies on
	// bad configuration dies in the first seconds — a wrong password
	// format, a data directory from another major version — and that is
	// the failure worth catching while somebody is still looking.
	ReadyAttempts int
	ReadyInterval time.Duration

	// meshNetwork answers whether this instance is a cluster, and with
	// what network. Nil until server.New wires one in.
	meshNetwork func(context.Context) string
}

func NewProvisioner(db *database.DB, docker DockerAPI, dataDir string) *Provisioner {
	return &Provisioner{
		db: db, docker: docker, dataDir: dataDir,
		ReadyAttempts: 10, ReadyInterval: time.Second,
		ExtensionAttempts: 60, ExtensionInterval: 2 * time.Second,
	}
}

func (p *Provisioner) repo() *Repository { return NewRepository(p.db) }

func (p *Provisioner) lock(id int64) *sync.Mutex {
	mu, _ := p.locks.LoadOrStore(id, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// DataDirFor is where this datastore's files live on the host.
//
// Keyed by id rather than by reference: the id is the one thing about a
// datastore that is not a name, and a directory named after a path
// nobody may rename is still a directory that has to survive somebody
// renaming the world around it.
func (p *Provisioner) DataDirFor(d *Datastore) string {
	if p.dataDir == "" {
		return ""
	}
	return filepath.Join(p.dataDir, "datastores", strconv.FormatInt(d.ID, 10))
}

// containerOpts is the whole configuration of a datastore's container.
//
// Nothing is published unless the datastore is exposed, and then it is
// published on every interface: an exposed database is one somebody
// means to reach from off this host, and a bind on loopback would be a
// port that answers only to the machine that did not need it.
func (p *Provisioner) containerOpts(ctx context.Context, d *Datastore) dockerx.ContainerOpts {
	opts := dockerx.ContainerOpts{
		Name:         ContainerName(d.Slug),
		Image:        d.Image(),
		Env:          d.ContainerEnv(),
		Cmd:          d.ContainerCmd(),
		Network:      Network,
		AlsoNetworks: p.mesh(ctx),
		Labels: map[string]string{
			// No Traefik labels: these speak their own wire protocol
			// over TCP, and Traefik routes HTTP by host name. What
			// identifies the container is here for whoever is reading
			// `docker ps` instead.
			"cubeship.datastore": d.Slug,
			"cubeship.engine":    string(d.Engine),
		},
	}
	// Informative and credential-free, like the two above: what somebody
	// reading `docker ps` needs is why this container is on an image
	// that is not plain `postgres:16`.
	if len(d.Extensions) > 0 {
		opts.Labels["cubeship.extensions"] = strings.Join(d.Extensions.Strings(), ",")
	}
	// Postgres takes its dynamic shared memory from /dev/shm, and the
	// Engine's 64 MiB default is too little for a large parallel query —
	// which fails as "could not resize shared memory segment" rather
	// than as anything about memory.
	if d.Engine == EnginePostgres {
		opts.ShmSize = PostgresShmSize
	}
	if dir := p.DataDirFor(d); dir != "" {
		opts.Binds = []string{dir + ":" + d.DataPath()}
	}
	if d.ExposedPort != 0 {
		opts.Ports = []string{fmt.Sprintf("%d:%d", d.ExposedPort, d.Engine.Port())}
	}
	opts.Resources = d.Limits.Resources()
	return opts
}

// Cap applies a new ceiling to the container that is already running,
// without replacing it or restarting the engine inside it.
//
// Every other setting on a datastore is either permanent or, like the
// exposed port, fixed when the container is created — publishing one
// replaces the container, which is a database going away for a few
// seconds. This one does not, because the Engine writes it straight to
// the cgroup.
//
// **Removing a limit is the direction that waits**, and here waiting
// means the next `start`: the Engine merges an update and reads a zero
// as "leave that one alone", so a container goes back to uncapped only
// by being created again.
func (p *Provisioner) Cap(ctx context.Context, d *Datastore) error {
	if d.ContainerID == "" {
		return nil
	}
	if err := p.docker.SetResources(ctx, d.ContainerID, d.Limits.Resources()); err != nil {
		// The limit is stored either way — it is what the next
		// container is created with. What failed is this one taking it
		// now, and a cgroup the host cannot enforce is a fact about
		// the machine rather than a transient error.
		return fmt.Errorf("the limit is saved, and %s did not take it: %w", ContainerName(d.Slug), err)
	}
	return nil
}

// Start provisions d in the background and returns immediately.
//
// Detached for the same reason a deploy is: pulling a database image is
// most of a minute on a fresh instance, and nothing useful happens by
// holding a connection open for it. How it went is on the datastore's
// own row, which is where every surface reads it from.
func (p *Provisioner) Start(d *Datastore) {
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		// An unrecovered panic here would take the daemon down and every
		// app it proxies with it, which is far worse than one database
		// that did not come up. It becomes this datastore's error.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("datastore %d: provision panicked: %v\n%s", d.ID, r, debug.Stack())
				p.fail(context.Background(), d, fmt.Errorf("provision panicked: %v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), ProvisionTimeout)
		defer cancel()

		if err := p.provision(ctx, d); err != nil {
			log.Printf("datastore %s: %v", d.Slug, err)
			p.fail(ctx, d, err)
		}
	}()
}

// Wait blocks until every provision this Provisioner has running has
// finished. For tests.
func (p *Provisioner) Wait() { p.running.Wait() }

func (p *Provisioner) fail(ctx context.Context, d *Datastore, cause error) {
	if err := p.repo().UpdateContainer(ctx, d.ID, "", StatusFailed, cause.Error()); err != nil {
		log.Printf("datastore %d: recording the failure failed too: %v", d.ID, err)
	}
}

func (p *Provisioner) provision(ctx context.Context, d *Datastore) error {
	mu := p.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()

	opts := p.containerOpts(ctx, d)

	if dir := p.DataDirFor(d); dir != "" {
		// Created here rather than left to Docker, which would create
		// it root-owned from the Engine's side. The engine images run
		// as root and drop privileges after taking ownership, so an
		// empty directory is all any of them need.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	// Whatever was there under this name goes first. A previous attempt
	// that failed after creating a container would otherwise make every
	// retry fail on the name, which is the one failure a retry should
	// fix. RemoveContainer forces, and nothing there is success — so
	// this only speaks up when the Engine actually refused.
	if err := p.docker.RemoveContainer(ctx, opts.Name); err != nil {
		log.Printf("datastore %s: could not clear %s before recreating it: %v", d.Slug, opts.Name, err)
	}

	// The pull is unconditional. There is no cheap "is this image here"
	// in the subset above, and a database image is pulled once per
	// version on a box — the round trip that finds it already present
	// costs less than the branch that guesses wrong.
	if err := p.docker.PullImage(ctx, opts.Image, nil); err != nil {
		log.Printf("datastore %s: pull %s failed, trying the local image (%v)", d.Slug, opts.Image, err)
	}

	id, err := p.docker.CreateContainer(ctx, opts)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}
	if err := p.docker.StartContainer(ctx, id); err != nil {
		// A container that would not start is not left lying about
		// under a name the next attempt needs.
		if rmErr := p.docker.RemoveContainer(ctx, id); rmErr != nil {
			log.Printf("datastore %s: abandoning a container that would not start: %v", d.Slug, rmErr)
		}
		return fmt.Errorf("start container: %w", err)
	}

	if err := p.repo().UpdateContainer(ctx, d.ID, id, StatusProvisioning, ""); err != nil {
		return err
	}

	if err := p.waitReady(ctx, id); err != nil {
		return err
	}
	// After the container is up and before it is called running: an app
	// that reads "running" and connects should find the extensions
	// there. A failure here fails the provision, which is what puts the
	// datastore in `failed` with the reason on its row.
	if err := p.installExtensions(ctx, d, id); err != nil {
		return err
	}
	return p.repo().UpdateContainer(ctx, d.ID, id, StatusRunning, "")
}

// waitReady watches a started container long enough to catch the
// failures that happen at once: a data directory written by another
// major version, a login the engine refused, an image whose entrypoint
// rejected its configuration. All of those exit within seconds.
//
// It is not a health check. Nothing here connects to the database — that
// would mean a client library per engine, and the thing that actually
// wants to know is the app, at the moment it connects.
func (p *Provisioner) waitReady(ctx context.Context, containerID string) error {
	for attempt := range p.ReadyAttempts {
		running, err := p.docker.IsRunning(ctx, containerID)
		if err != nil {
			return fmt.Errorf("inspect container: %w", err)
		}
		if !running {
			return fmt.Errorf("the database exited on startup: %s", p.tail(ctx, containerID))
		}
		if attempt == p.ReadyAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.ReadyInterval):
		}
	}
	return nil
}

// tail is the end of a container's log, which is where an engine says
// why it refused to start. Without it the failure is "exited", and the
// only explanation is in a container that has since been removed.
func (p *Provisioner) tail(ctx context.Context, containerID string) string {
	rc, err := p.docker.Logs(ctx, containerID, "20")
	if err != nil {
		return "no log available: " + err.Error()
	}
	defer rc.Close()

	var out strings.Builder
	if _, err := stdcopy.StdCopy(&out, &out, rc); err != nil {
		return "no log available: " + err.Error()
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "it wrote nothing to its log"
	}
	return text
}

// Stop turns a datastore off without removing it.
//
// Stopped rather than removed, so the container keeps whatever the
// Engine knows about it — its logs above all, which are the first thing
// anybody wants after something goes quiet. Docker's restart policy is
// unless-stopped, so a container stopped here stays stopped across a
// reboot: turning it back on is a decision, not something a power cut
// makes for you.
func (p *Provisioner) Stop(ctx context.Context, d *Datastore) error {
	mu := p.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()

	// By name rather than by the recorded id, because the name is the
	// one that is right even when the row is stale.
	if err := p.docker.StopContainer(ctx, ContainerName(d.Slug)); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}
	return p.repo().UpdateContainer(ctx, d.ID, d.ContainerID, StatusStopped, "")
}

// Logs is what the engine has written, most recent `tail` lines.
//
// Read from the container by name, so a datastore whose row remembers a
// container that has since been replaced still answers with the one
// running now.
func (p *Provisioner) Logs(ctx context.Context, d *Datastore, tail string) (io.ReadCloser, error) {
	return p.docker.Logs(ctx, ContainerName(d.Slug), tail)
}

// Teardown removes a datastore's container, and its data with it when
// keepData is false.
//
// Synchronous, unlike provisioning: whoever asked for this is being
// told whether it worked, and a delete that reports success while the
// container is still serving would be a lie somebody acts on.
func (p *Provisioner) Teardown(ctx context.Context, d *Datastore, keepData bool) error {
	mu := p.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()

	// By name rather than by the recorded id, because the name is the
	// one that is right even if the row is stale — and the row is
	// exactly what is about to be deleted.
	name := ContainerName(d.Slug)
	if err := p.docker.RemoveContainer(ctx, name); err != nil {
		log.Printf("datastore %s: removing %s: %v", d.Slug, name, err)
	}
	if keepData {
		return nil
	}

	dir := p.DataDirFor(d)
	// Never the data directory itself, only a subdirectory of it. A
	// Provisioner built without one removes nothing rather than
	// resolving to "/".
	if dir == "" || filepath.Clean(dir) == filepath.Clean(p.dataDir) {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove data directory: %w", err)
	}
	return nil
}

// SetMeshNetwork wires in what says whether this instance is a cluster.
//
// It answers with the cluster overlay's name when there is one and with
// nothing when there is not, and what it decides is one extra network
// on every container created here — the local bridge stays, because
// everything on this box already resolves the container there.
//
// A function rather than an interface because that is the whole of it,
// and `server.New` is the only caller. See internal/mesh.
func (p *Provisioner) SetMeshNetwork(fn func(context.Context) string) { p.meshNetwork = fn }

// mesh is the network to also join, or none. Nothing is asked of the
// Engine when nobody wired one in, which is every test.
func (p *Provisioner) mesh(ctx context.Context) []string {
	if p.meshNetwork == nil {
		return nil
	}
	if name := p.meshNetwork(ctx); name != "" {
		return []string{name}
	}
	return nil
}

// Install creates this datastore's extensions in the container it is
// already running, without replacing it.
//
// The path for a contrib module: it is in the image already, so all that
// is missing is the statement. Synchronous, because it is one statement
// against a server that is up — whoever asked is still waiting, and the
// answer is worth having.
func (p *Provisioner) Install(ctx context.Context, d *Datastore) error {
	mu := p.lock(d.ID)
	mu.Lock()
	defer mu.Unlock()

	if d.ContainerID == "" {
		return ErrNotRunning
	}
	return p.installExtensions(ctx, d, d.ContainerID)
}

// NeedsReplacement reports whether going from one set of extensions to
// another means a new container rather than a statement.
//
// Two things decide it, and both are fixed when a container is created:
// the image, and what the postmaster preloads. Everything else — every
// contrib module — is already on disk beside the server.
func NeedsReplacement(before, after *Datastore) bool {
	return before.Image() != after.Image() ||
		!slices.Equal(before.ContainerCmd(), after.ContainerCmd())
}

// installExtensions creates this datastore's extensions inside the
// container that has just come up.
//
// **Nothing here is built from input.** The statements are constants
// picked by name from the support matrix, the database and the login are
// the ones this instance created, and the order is the matrix's — so the
// whole of what a request decides is which of a handful of fixed
// statements run.
//
// It runs on **every** provision, not only the first. Publishing a port
// replaces the container and `start` recreates it, and both land on the
// same data directory where the extensions already exist: every
// statement is `IF NOT EXISTS`, so the second time through is a no-op
// that still proves they are there.
//
// **No password goes anywhere.** psql connects over the container's own
// Unix socket, which the official image's initdb trusts, and the login
// Cubeship created is the superuser that CREATE EXTENSION needs. The
// alternative — PGPASSWORD in the exec's argv, the way a dump does it —
// would put the credential in a process list for no gain, since a
// process inside this container can already read it from its own
// environment.
func (p *Provisioner) installExtensions(ctx context.Context, d *Datastore, containerID string) error {
	plan := d.InstallPlan()
	if len(plan) == 0 {
		return nil
	}
	if err := p.waitAccepting(ctx, d, containerID); err != nil {
		return err
	}
	for _, step := range plan {
		if out, err := p.exec(ctx, containerID, psqlCmd(d, step.SQL)); err != nil {
			return fmt.Errorf("creating the %s extension failed: %w%s", step.Extension, err, detail(out))
		}
	}
	return nil
}

// waitAccepting blocks until the engine answers on its own TCP port.
//
// Over TCP rather than the socket, and that is the whole reason this is
// not a one-liner: the Postgres image initializes a new data directory
// by starting a temporary server that listens on the socket **only**,
// runs what it has to, then stops it and starts the real one. A socket
// that answers therefore means nothing — a CREATE EXTENSION sent to that
// server is thrown away with it, and the datastore comes up reporting
// extensions it does not have. Nothing listens on the port until the
// server that keeps its work is up.
func (p *Provisioner) waitAccepting(ctx context.Context, d *Datastore, containerID string) error {
	port := strconv.Itoa(d.Engine.Port())
	var last string
	for attempt := range p.ExtensionAttempts {
		out, err := p.exec(ctx, containerID,
			[]string{"pg_isready", "-h", "127.0.0.1", "-p", port, "-U", d.Username, "-q"})
		if err == nil {
			return nil
		}
		last = strings.TrimSpace(out + " " + err.Error())
		if attempt == p.ExtensionAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.ExtensionInterval):
		}
	}
	return fmt.Errorf("the database did not start accepting connections, so its extensions were not created: %s", last)
}

// psqlCmd is the client invocation for one statement.
//
// ON_ERROR_STOP is what makes a failed statement a non-zero exit: psql's
// default is to print the error and carry on, which would report success
// for a database with no extensions in it.
//
// The socket directory is named rather than left to psql's default,
// because the default is a compile-time path and being explicit is what
// makes this readable next to the note above about which server answers
// where.
func psqlCmd(d *Datastore, sql string) []string {
	return []string{
		"psql", "--no-psqlrc", "-v", "ON_ERROR_STOP=1",
		"-h", "/var/run/postgresql", "-U", d.Username, "-d", d.Database,
		"-c", sql,
	}
}

// exec runs one command in the container and returns what it wrote.
//
// Output and stderr together, because what psql says about a missing
// library is on one of the two depending on the version, and the whole
// point of returning it is that somebody reads the reason on the
// datastore's own row.
func (p *Provisioner) exec(ctx context.Context, containerID string, cmd []string) (string, error) {
	var out strings.Builder
	stderr, code, err := p.docker.ExecStream(ctx, containerID, cmd, nil, &out)
	text := strings.TrimSpace(out.String() + "\n" + strings.TrimSpace(stderr))
	if err != nil {
		return text, err
	}
	if code != 0 {
		return text, fmt.Errorf("%s exited with status %d", cmd[0], code)
	}
	return text, nil
}

// detail appends what the engine said, when it said anything. Never a
// credential: nothing this file runs is given one.
func detail(out string) string {
	if out = strings.TrimSpace(out); out == "" {
		return ""
	}
	return ": " + out
}
