package node

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/distribution/reference"

	"cubeship/internal/firewall"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/user"
)

// Service is the cluster: which machines are in it, and what the
// workers are told when they call.
//
// It sits at the bottom with `user` and knows about no other module.
// What runs *on* a node is the placing module's business, and it will
// reach this one through the Desired the agent already asks for.
type Service struct {
	db      *database.DB
	machine *machine.Service

	// hub is what lets this instance ask a machine something. See
	// commands.go: the answer to a machine's own poll is the only way
	// in, because nothing here can dial one.
	hub *hub

	// engine is what brings the cluster's private network up on this
	// machine. Nil on a daemon whose Docker cannot cluster and in every
	// test, and a cluster with no mesh is exactly that: machines that
	// report in and share no network.
	engine mesh.Engine
	// host is how the cluster's ports are opened on **this** machine.
	// The workers open their own; see internal/worker.
	host firewall.Host
	// apps knows what should be running where. Nil until server.New
	// wires it in, and then a cluster is machines that report in and
	// are told to run nothing.
	apps Apps
	// registry answers where this instance's own registry is, so a
	// machine knows which images it should authenticate as itself for.
	registry func(ctx context.Context) string
	storage  StorageEngine
	// builderCache is the image builder's own layer store, which the
	// Engine does not report and cannot clean. Nil on a server without a
	// builder.
	builderCache BuilderCache
	// advertise is where the other machines reach this one. It is the
	// instance's own public address, which settings already works out
	// and refuses to guess badly — a bridge address here would build a
	// cluster nothing can join.
	advertise func(ctx context.Context) string

	// admitted is the peer set this machine's firewall was last opened
	// for. Writing a ufw rule costs a container through hostexec, so it
	// is done when the cluster changes rather than on every pass.
	mu       sync.Mutex
	admitted string
	// networkAt is when the Engine was last asked whether the cluster's
	// overlay is there. Every container this instance creates asks, and
	// the answer changes about once in the life of an instance.
	networkAt   time.Time
	networkName string
}

type StorageEngine interface {
	Storage(context.Context) (dockerx.Storage, error)
	Prune(context.Context) (int64, error)
}

// BuilderCache is the layer cache of the image builder.
//
// It is a store of its own, beside the Engine's: the build cache the
// Engine reports is empty on an instance that builds only through
// BuildKit, while this one grows by a build's worth of layers per deploy.
type BuilderCache interface {
	// CacheUsage is the bytes a clean would free.
	CacheUsage(context.Context) (int64, error)
	// PruneCache removes what no running build holds, and returns the
	// bytes freed.
	PruneCache(context.Context) (int64, error)
}

// meshLookupTTL is how long "is there a cluster network" is believed.
//
// The question is asked on every container this instance creates, and
// the answer changes when somebody adds the first machine — so it is
// worth not asking the Engine per deploy, and not worth a subscription.
const meshLookupTTL = 30 * time.Second

func NewService(db *database.DB) *Service { return &Service{db: db, hub: newHub()} }

func (s *Service) SetMachine(machine *machine.Service) { s.machine = machine }

func (s *Service) SetStorage(engine StorageEngine) { s.storage = engine }

func (s *Service) SetBuilderCache(cache BuilderCache) { s.builderCache = cache }

func (s *Service) Storage(ctx context.Context, caller *user.User, name string) (dockerx.Storage, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelView, ""); err != nil {
		return dockerx.Storage{}, err
	}
	n, err := s.Get(ctx, caller, name)
	if err != nil {
		return dockerx.Storage{}, err
	}
	if !n.ControlPlane {
		return dockerx.Storage{}, ErrControlPlane
	}
	if s.storage == nil {
		return dockerx.Storage{}, errors.New("docker storage is unavailable")
	}
	storage, err := s.storage.Storage(ctx)
	if err != nil {
		return dockerx.Storage{}, err
	}
	return withBuilderCache(ctx, storage, s.builderCache), nil
}

// withBuilderCache counts the builder's layers as build cache, which is
// what they are: the card must read what Clean will free. A builder that
// does not answer leaves the Engine's numbers as they were.
func withBuilderCache(ctx context.Context, storage dockerx.Storage, cache BuilderCache) dockerx.Storage {
	if cache == nil {
		return storage
	}
	bytes, err := cache.CacheUsage(ctx)
	if err != nil {
		log.Printf("node: builder cache size: %v", err)
		return storage
	}
	storage.BuildCacheBytes += bytes
	return storage
}

// pruneStorage cleans the Engine, then the builder. A failure in the
// builder still reports what the Engine freed.
func pruneStorage(ctx context.Context, engine StorageEngine, cache BuilderCache) (int64, error) {
	reclaimed, err := engine.Prune(ctx)
	if err != nil {
		return 0, err
	}
	if cache == nil {
		return reclaimed, nil
	}
	freed, err := cache.PruneCache(ctx)
	return reclaimed + freed, err
}

func (s *Service) PruneStorage(ctx context.Context, caller *user.User, name string) (int64, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelManage, ""); err != nil {
		return 0, err
	}
	n, err := s.Get(ctx, caller, name)
	if err != nil {
		return 0, err
	}
	if !n.ControlPlane {
		return 0, ErrControlPlane
	}
	if s.storage == nil {
		return 0, errors.New("docker storage is unavailable")
	}
	return pruneStorage(ctx, s.storage, s.builderCache)
}

// SetMesh wires in what the cluster's network is made of. Called once,
// by server.New, with whatever this daemon actually has: a Docker that
// can cluster, a way to reach the host's firewall, and this instance's
// own address.
func (s *Service) SetMesh(engine mesh.Engine, host firewall.Host, advertise func(context.Context) string) {
	s.engine, s.host, s.advertise = engine, host, advertise
}

// SetApps wires in what knows which apps belong on which machine.
// Called once, by server.New — the module that owns apps sits above
// this one, so it is handed back down here.
func (s *Service) SetApps(a Apps) { s.apps = a }

// SetRegistryHost wires in where this instance's own registry answers.
// It follows the instance's domain, so it is asked rather than captured.
func (s *Service) SetRegistryHost(fn func(context.Context) string) { s.registry = fn }

// RegistryHost is the address an image pushed to this instance is
// pulled from, or empty when there is no domain and therefore no
// registry.
func (s *Service) RegistryHost(ctx context.Context) string {
	if s.registry == nil {
		return ""
	}
	return s.registry(ctx)
}

// Logs is the tail of a container's log, read on the machine it is on.
//
// It is the first thing that goes through the reverse channel, and the
// shape is the point: the request parks here, the machine's next poll
// carries the command, and its answer releases this. What comes back is
// bytes, already demultiplexed out of Docker's frame format by the
// machine that read them — so what a caller does with this is what it
// does with a local log, and internal/app does not branch on which
// machine an app is on beyond choosing this door.
func (s *Service) Logs(ctx context.Context, nodeID int64, containerID, tail string) ([]byte, error) {
	if containerID == "" {
		return nil, ErrNoContainer
	}
	return s.hub.Ask(ctx, nodeID, Command{
		Kind: CommandLogs, Container: containerID, Tail: tail,
	})
}

// Addresses is where each machine in the cluster is reached, by id.
//
// One query rather than one per app: what asks is a listing, and a
// listing that resolved a machine per row would be a listing that gets
// slower as the cluster grows. A machine that has not reported an
// address is not in the answer at all — there is nothing to point a
// name at, and an empty string would read as one.
func (s *Service) Addresses(ctx context.Context) (map[int64]string, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(all))
	for _, n := range all {
		if n.Address != "" {
			out[n.ID] = n.Address
		}
	}
	return out, nil
}

// Quiet is the machines that have not called in for at least d, by id.
//
// The caller says how patient it wants to be, because the two questions
// are not the same: three missed passes is enough to stop *routing* to
// a machine and nowhere near enough to conclude that something waiting
// on it is never going to happen. `unreachable` is the first; this is
// how the second is asked.
//
// Derived from last_seen_at on every read, like Node.Status itself, so
// it cannot go stale the way a stored column would. Only the machines
// that are actually quiet are in it: a caller reads a missing key as
// "it is answering", which is what an instance with no cluster gets.
//
// A machine that has **never** called in is quiet however long d is —
// it is a row somebody made and never installed, and something waiting
// on it is waiting for a box that does not exist.
//
// The control plane is never in the answer. It cannot be out of touch
// with itself, and a daemon reporting its own box missing would be one
// deciding something is wrong from the only place that can be sure it
// is not.
func (s *Service) Quiet(ctx context.Context, d time.Duration) (map[int64]bool, error) {
	all, err := s.Repo().List(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, n := range all {
		switch {
		case n.ControlPlane:
		case n.LastSeenAt == nil, time.Since(*n.LastSeenAt) >= d:
			out[n.ID] = true
		}
	}
	return out, nil
}

// Wake tells a machine there is something new for it, without waiting
// for anything back.
//
// What calls it is a deploy placed on that machine: the poll it releases
// is the difference between a deploy starting now and one starting when
// the machine next asks.
func (s *Service) Wake(nodeID int64) { s.hub.Signal(nodeID) }

// AuthenticateNode returns a stable identity for placement authorization and
// the display name used as the registry token subject.
func (s *Service) AuthenticateNode(ctx context.Context, token string) (int64, string, error) {
	n, err := s.Authenticate(ctx, token)
	if err != nil {
		return 0, "", err
	}
	return n.ID, n.Slug, nil
}

// NodeCanPull authorizes the repositories in this machine's desired images.
// PlacementsFor follows pending deploys and falls back after a failed deploy.
// Errors must deny access, including errors resolving a placement's image.
func (s *Service) NodeCanPull(ctx context.Context, nodeID int64, repository string) (bool, error) {
	if s.apps == nil {
		return false, nil
	}
	host := s.RegistryHost(ctx)
	if host == "" {
		return false, nil
	}
	placements, err := s.apps.PlacementsFor(ctx, nodeID)
	if err != nil {
		return false, err
	}
	for _, placement := range placements {
		if registryRepository(placement.Image, host) == repository && repository != "" {
			return true, nil
		}
	}
	return false, nil
}

// Match the complete registry authority and repository, never a substring.
// Tags and digests are not expressible in distribution's repository scopes.
func registryRepository(image, host string) string {
	authority, _, ok := strings.Cut(image, "/")
	if !ok || authority != host || host == "" {
		return ""
	}
	named, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return ""
	}
	return reference.Path(named)
}

func (s *Service) Repo() *Repository { return NewRepository(s.db) }

// Add puts a machine in the cluster and mints the credential its agent
// will authenticate with.
//
// **The credential is returned once and cannot be read again**, like an
// API key: only its hash is stored. Losing it means removing the node
// and adding it back, which is the same cost as rotating it and one
// fewer thing to build.
//
// Nothing is contacted. The row is a place for a machine that does not
// exist yet — somebody now goes and runs the installer on it — and a
// node that has never called is `pending` rather than an error.
func (s *Service) Add(ctx context.Context, caller *user.User, name, description string) (*Node, string, error) {
	if err := user.Allow(caller, user.ResServers, user.LevelManage, ""); err != nil {
		return nil, "", err
	}
	if err := checkSlug(name); err != nil {
		return nil, "", err
	}
	token, err := authkey.Generate()
	if err != nil {
		return nil, "", fmt.Errorf("generate the server's credential: %w", err)
	}
	// The cluster's network comes up now rather than when the machine
	// first calls, because this is the moment somebody is watching. A
	// swarm that cannot start — no public address to advertise, a
	// Docker already in somebody else's swarm — is a machine that would
	// join and never reach anything, and finding that out from a
	// refusal here beats finding it out from a screen that says `ready`.
	if _, err := s.mesh(ctx); err != nil {
		return nil, "", err
	}

	created, err := s.Repo().Create(ctx, name, description, authkey.Hash(token))
	if err != nil {
		return nil, "", err
	}
	// The cluster has one more machine, so whatever follows it is due a
	// share. A failure here is a log line rather than an undone add:
	// the machine is in the cluster either way, and the next placement
	// or the next add re-spreads. Refusing the add would be undoing
	// something that worked because something else did not.
	s.rebalance(ctx, 0, "adding "+name)
	return created, token, nil
}
