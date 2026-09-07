package objectstore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
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

// Image is the server a managed store runs.
//
// **Pinned, and the pin is not a chore here.** The community image
// stopped moving — MinIO's development went to a product this is not —
// so this tag is where the free server ends rather than a snapshot of
// something that will be stale next month. Following `latest` would
// have meant a server that changes underneath somebody's data on a
// redeploy, which is the one thing a store must not do.
const Image = "minio/minio"

// Versions are the releases this Cubeship offers, newest first. The
// first is what a store created without naming one runs.
//
// A version is permanent once a store holds data, for the reason a
// datastore's is: the directory belongs to the server that wrote it.
func Versions() []string {
	return []string{"RELEASE.2025-09-07T16-13-09Z"}
}

// DefaultVersion is what a store created without naming one runs.
func DefaultVersion() string { return Versions()[0] }

// KnowsVersion reports whether this release offers a version.
func KnowsVersion(v string) bool {
	for _, known := range Versions() {
		if known == v {
			return true
		}
	}
	return false
}

// ImageFor is the full reference for a version.
func ImageFor(version string) string { return Image + ":" + version }

// DataPath is where MinIO keeps its objects inside the container, and
// so what the host directory is mounted over.
//
// The mount point itself, not a directory below it. That is the same
// trap the database engines set: an image that chowns its data
// directory and then drops privileges cannot traverse a mount above it
// that is still root-owned. MinIO runs as root and does not chown, so
// this works either way — and pointing it deeper would be inviting the
// problem back for no gain.
const DataPath = "/data"

// DockerAPI is the subset of dockerx.Client a store's container needs.
// The same set the datastore provisioner takes, so the daemon hands one
// client to both and a test that fakes Docker fakes it once.
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
// timeout — the caller stopped waiting long ago — it only stops a
// wedged pull from running forever.
const ProvisionTimeout = 15 * time.Minute

// Provisioner owns a managed store's container, and is the only thing
// here that creates or removes one. An external store never reaches it.
type Provisioner struct {
	db     *database.DB
	docker DockerAPI
	// dataDir is the instance's state directory *on the host*: the
	// daemon hands these paths to the Engine, which resolves them
	// there. That is why the data directory is mounted at the same path
	// inside the daemon's container and outside it.
	dataDir string

	locks   sync.Map
	running sync.WaitGroup

	// ReadyAttempts and ReadyInterval bound how long a provision
	// watches a started container before calling it up. A server that
	// refuses its own configuration — a root password under eight
	// characters, a data directory it cannot write — dies in the first
	// seconds, and that is the failure worth catching while somebody is
	// still looking at the screen.
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

// Wait blocks until every provision this Provisioner started has
// finished. For tests.
func (p *Provisioner) Wait() { p.running.Wait() }

// DataDirFor is where this store's objects live on the host.
//
// Keyed by id rather than by name, like a datastore's: the id is the
// one thing about a store that is not a name, and a directory that
// survives somebody renaming the world around it is worth the
// indirection.
func (p *Provisioner) DataDirFor(s *Store) string {
	if p.dataDir == "" {
		return ""
	}
	return filepath.Join(p.dataDir, "objectstores", strconv.FormatInt(s.ID, 10))
}

// containerOpts is the whole configuration of a managed store's
// container.
func (p *Provisioner) containerOpts(s *Store) dockerx.ContainerOpts {
	opts := dockerx.ContainerOpts{
		Name:  ContainerName(s.Slug),
		Image: ImageFor(s.Version),
		// One directory, which is MinIO's single-node single-drive
		// mode. Erasure coding wants several drives and there is one
		// disk on this machine — spreading a bucket across four
		// directories on it would buy the ceremony of redundancy and
		// none of the redundancy.
		Cmd: []string{"server", DataPath},
		Env: []string{
			"MINIO_ROOT_USER=" + s.AccessKey,
			"MINIO_ROOT_PASSWORD=" + s.SecretKey,
		},
		Network: Network,
		Labels: map[string]string{
			// No Traefik labels. A managed store answers to apps on the
			// shared network by container name, and to anything else
			// through the host port below — routing it by hostname
			// would need a name that is unique across every app on the
			// instance, which is a decision this module does not own.
			"cubeship.objectstore": s.Slug,
		},
	}
	if dir := p.DataDirFor(s); dir != "" {
		opts.Binds = []string{dir + ":" + DataPath}
	}
	if s.ExposedPort != 0 {
		opts.Ports = []string{fmt.Sprintf("%d:%d", s.ExposedPort, Port)}
	}
	return opts
}

// Start provisions s in the background and returns immediately.
//
// Detached for the same reason a deploy is: pulling this image is most
// of a minute on a fresh instance, and nothing useful happens by
// holding a connection open for it. How it went is on the store's own
// row, which is where every surface reads it from.
func (p *Provisioner) Start(s *Store) {
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		// An unrecovered panic here would take the daemon down and
		// every app it proxies with it, which is far worse than one
		// store that did not come up. It becomes this store's error.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("object store %d: provision panicked: %v\n%s", s.ID, r, debug.Stack())
				p.fail(context.Background(), s, fmt.Errorf("provision panicked: %v", r))
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), ProvisionTimeout)
		defer cancel()

		if err := p.provision(ctx, s); err != nil {
			log.Printf("object store %s: %v", s.Slug, err)
			p.fail(ctx, s, err)
		}
	}()
}

func (p *Provisioner) fail(ctx context.Context, s *Store, cause error) {
	if err := p.repo().UpdateContainer(ctx, s.ID, "", StatusFailed, cause.Error()); err != nil {
		log.Printf("object store %d: recording the failure failed too: %v", s.ID, err)
	}
}

func (p *Provisioner) provision(ctx context.Context, s *Store) error {
	mu := p.lock(s.ID)
	mu.Lock()
	defer mu.Unlock()

	opts := p.containerOpts(s)

	if dir := p.DataDirFor(s); dir != "" {
		// Created here rather than left to Docker, which would create
		// it from the Engine's side with no say in the mode.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	// Whatever was there under this name goes first: a previous attempt
	// that failed after creating a container would otherwise make every
	// retry fail on the name, which is the one failure a retry should
	// fix. Nothing there is success, so this only speaks up when the
	// Engine actually refused.
	if err := p.docker.RemoveContainer(ctx, opts.Name); err != nil {
		log.Printf("object store %s: could not clear %s before recreating it: %v", s.Slug, opts.Name, err)
	}

	if err := p.docker.PullImage(ctx, opts.Image, nil); err != nil {
		log.Printf("object store %s: pull %s failed, trying the local image (%v)", s.Slug, opts.Image, err)
	}

	id, err := p.docker.CreateContainer(ctx, opts)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}
	if err := p.docker.StartContainer(ctx, id); err != nil {
		if rmErr := p.docker.RemoveContainer(ctx, id); rmErr != nil {
			log.Printf("object store %s: abandoning a container that would not start: %v", s.Slug, rmErr)
		}
		return fmt.Errorf("start container: %w", err)
	}

	if err := p.repo().UpdateContainer(ctx, s.ID, id, StatusProvisioning, ""); err != nil {
		return err
	}
	if err := p.waitReady(ctx, id); err != nil {
		return err
	}
	return p.repo().UpdateContainer(ctx, s.ID, id, StatusRunning, "")
}

// waitReady watches a started container long enough to catch the
// failures that happen at once.
//
// It is not a health check, and deliberately does not connect: the
// thing that actually wants to know whether the store answers is the
// screen that lists its buckets, at the moment it asks.
func (p *Provisioner) waitReady(ctx context.Context, containerID string) error {
	for attempt := range p.ReadyAttempts {
		running, err := p.docker.IsRunning(ctx, containerID)
		if err != nil {
			return fmt.Errorf("inspect container: %w", err)
		}
		if !running {
			return fmt.Errorf("the store exited on startup: %s", p.tail(ctx, containerID))
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

// tail is the end of a container's log, which is where a server says
// why it refused to start. Without it the failure is "exited", and the
// only explanation is inside a container that has since been removed.
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

// Stop turns a store off without removing it, so its log survives —
// what somebody wants immediately after turning storage off is usually
// the reason they turned it off.
func (p *Provisioner) Stop(ctx context.Context, s *Store) error {
	mu := p.lock(s.ID)
	mu.Lock()
	defer mu.Unlock()

	// By name rather than by the recorded id, because the name is the
	// one that is right even when the row is stale.
	if err := p.docker.StopContainer(ctx, ContainerName(s.Slug)); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}
	return p.repo().UpdateContainer(ctx, s.ID, s.ContainerID, StatusStopped, "")
}

// Logs is what the server has written, most recent `tail` lines.
func (p *Provisioner) Logs(ctx context.Context, s *Store, tail string) (io.ReadCloser, error) {
	return p.docker.Logs(ctx, ContainerName(s.Slug), tail)
}

// Teardown removes a store's container, and its objects with it when
// keepData is false.
//
// Synchronous, unlike provisioning: whoever asked for this is being
// told whether it worked, and a delete that reports success while the
// server is still serving would be a lie somebody acts on.
func (p *Provisioner) Teardown(ctx context.Context, s *Store, keepData bool) error {
	mu := p.lock(s.ID)
	mu.Lock()
	defer mu.Unlock()

	name := ContainerName(s.Slug)
	if err := p.docker.RemoveContainer(ctx, name); err != nil {
		log.Printf("object store %s: removing %s: %v", s.Slug, name, err)
	}
	if keepData {
		return nil
	}

	dir := p.DataDirFor(s)
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

// Reconcile corrects each managed store's recorded status against what
// Docker is actually running. It runs at startup, when the database may
// describe a world from before a reboot.
//
// Like every other reconciler here it never starts, stops or removes
// anything: correcting the record is safe, and acting on a stale one is
// how a reconciler takes down something that was working.
func Reconcile(ctx context.Context, repo *Repository, d interface {
	IsRunning(ctx context.Context, id string) (bool, error)
}) error {
	all, err := repo.List(ctx)
	if err != nil {
		return err
	}
	for _, s := range all {
		if s.Kind != KindManaged || s.ContainerID == "" {
			continue
		}
		running, err := d.IsRunning(ctx, s.ContainerID)
		if err != nil {
			log.Printf("reconcile: object store %s: inspect container %s failed: %v",
				s.Slug, s.ContainerID, err)
			running = false
		}
		want := StatusDown
		if running {
			want = StatusRunning
		}
		// A store that failed to provision keeps saying so: there is no
		// container to have gone away, and "down" would lose the only
		// explanation anybody has. A stopped one keeps saying so too,
		// because Docker's unless-stopped policy means it is still off
		// on purpose after a reboot.
		if !running && (s.Status == StatusFailed || s.Status == StatusStopped) {
			continue
		}
		if want != s.Status {
			log.Printf("reconcile: object store %s: status %s -> %s", s.Slug, s.Status, want)
			if err := repo.UpdateContainer(ctx, s.ID, s.ContainerID, want, s.Error); err != nil {
				return err
			}
		}
	}
	return nil
}

// Key generation. A managed store's keys are minted here and shown
// once, like a database's password: a field somebody has to fill in is
// a field somebody fills in badly, and this one is on the open internet
// the moment the store is exposed.
const (
	// AccessKeyLength is what AWS's own are, which is what every tool
	// and every example is shaped around.
	AccessKeyLength = 20
	SecretKeyLength = 40
)

// The alphabets are deliberately narrow. A secret ends up in a
// container's environment, in a connection string somebody pastes into
// a terminal, and in a config file — and every character that has to be
// escaped somewhere is a character that eventually is not.
const (
	accessAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// GenerateKeys mints the pair a managed store is initialized with.
func GenerateKeys() (accessKey, secretKey string, err error) {
	if accessKey, err = randomString(accessAlphabet, AccessKeyLength); err != nil {
		return "", "", err
	}
	if secretKey, err = randomString(secretAlphabet, SecretKeyLength); err != nil {
		return "", "", err
	}
	return accessKey, secretKey, nil
}

func randomString(alphabet string, length int) (string, error) {
	limit := big.NewInt(int64(len(alphabet)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", errors.New("generate key: " + err.Error())
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}
