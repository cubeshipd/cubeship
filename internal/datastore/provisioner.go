package datastore

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
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
	IsRunning(ctx context.Context, id string) (bool, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
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

	// ReadyAttempts and ReadyInterval bound how long a provision watches
	// a started container before calling it up. An engine that dies on
	// bad configuration dies in the first seconds — a wrong password
	// format, a data directory from another major version — and that is
	// the failure worth catching while somebody is still looking.
	ReadyAttempts int
	ReadyInterval time.Duration
}

func NewProvisioner(db *database.DB, docker DockerAPI, dataDir string) *Provisioner {
	return &Provisioner{
		db: db, docker: docker, dataDir: dataDir,
		ReadyAttempts: 10, ReadyInterval: time.Second,
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
func (p *Provisioner) containerOpts(d *Datastore) dockerx.ContainerOpts {
	opts := dockerx.ContainerOpts{
		Name:    ContainerName(d.Slug),
		Image:   d.Engine.Image(d.Version),
		Env:     d.ContainerEnv(),
		Cmd:     d.ContainerCmd(),
		Network: Network,
		Labels: map[string]string{
			// No Traefik labels: these speak their own wire protocol
			// over TCP, and Traefik routes HTTP by host name. What
			// identifies the container is here for whoever is reading
			// `docker ps` instead.
			"cubeship.datastore": d.Slug,
			"cubeship.engine":    string(d.Engine),
		},
	}
	if dir := p.DataDirFor(d); dir != "" {
		opts.Binds = []string{dir + ":" + d.DataPath()}
	}
	if d.ExposedPort != 0 {
		opts.Ports = []string{fmt.Sprintf("%d:%d", d.ExposedPort, d.Engine.Port())}
	}
	return opts
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

	opts := p.containerOpts(d)

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
	// fix. RemoveContainer forces, and "no such container" is the
	// normal answer.
	if err := p.docker.RemoveContainer(ctx, opts.Name); err != nil {
		log.Printf("datastore %s: nothing to remove under %s (%v)", d.Slug, opts.Name, err)
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
