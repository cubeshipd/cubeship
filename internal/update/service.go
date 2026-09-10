package update

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/release"
	"cubeship/internal/user"
)

// Docker is what this module needs from the Engine.
type Docker interface {
	PullImage(ctx context.Context, ref string, creds *dockerx.RegistryAuth) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	SpecOf(ctx context.Context, name string) (dockerx.ContainerOpts, error)
}

// Cluster is what this module needs from the machines.
//
// Declared here and satisfied by `node` at wiring time, the direction
// every seam in this codebase runs: the module that owns the rows is
// the one that can answer, and it sits below this one.
type Cluster interface {
	// Workers are the machines that are not this one, by id, with the
	// version each last reported.
	Workers(ctx context.Context) (map[int64]string, error)
	// Update tells one machine to replace itself with a version. It
	// returns as soon as the machine has been told: a machine
	// replacing itself stops answering, so there is nothing to wait
	// for on the call.
	Update(ctx context.Context, nodeID int64, version string) error
}

// Service is the whole of updating an instance.
type Service struct {
	store   Store
	docker  Docker
	cluster Cluster
	// version is what this build is. A build with nothing stamped on
	// it is a developer's, and has nothing to update from.
	version string
	// daemon and frontend are the container names to replace, and the
	// image each is pulled from — the image being what the daemon was
	// told, not what it derives, for the reason install.sh passes it.
	daemon, frontend           string
	daemonImage, frontendImage string
	// client is how releases are looked up. Replaceable for a test.
	client *http.Client

	// running guards against two updates at once from one process. The
	// file guards against everything else — a second daemon, a browser
	// that reloaded — and this is the cheap half.
	running sync.Mutex
}

func NewService(dataDir string, d Docker, version, daemon, daemonImage, frontend, frontendImage string) *Service {
	return &Service{
		store: Store{DataDir: dataDir}, docker: d,
		version:  release.Normalize(version),
		daemon:   daemon,
		frontend: frontend, daemonImage: daemonImage, frontendImage: frontendImage,
	}
}

// SetCluster wires in the machines. Called once, by server.New.
func (s *Service) SetCluster(c Cluster) { s.cluster = c }

// SetClient replaces how releases are looked up. For a test.
func (s *Service) SetClient(c *http.Client) { s.client = c }

// Version is what this instance is running.
func (s *Service) Version() string { return s.version }

// Current is the run on disk, or nil.
//
// **It takes no caller and checks no role**, and it is the one thing
// here that does not: the middleware that refuses writes during an
// update asks it on every request, including from somebody not signed
// in — and a sign-in page that cannot tell it is talking to an instance
// mid-update is a sign-in that fails for no visible reason.
func (s *Service) Current() *Run {
	r := s.store.Read()
	if r.Stale(StuckAfter) {
		// Nothing is coming to close it. See Stale: the only way to get
		// here is the updater dying in the one moment nothing is left
		// to write the file, and an instance locked for ever is worse
		// than one that decides an update is over.
		return nil
	}
	return r
}

// State is what a screen shows.
type State struct {
	// Version is what this instance is running.
	Version string
	// Available is a newer stable release, or nil. Nil is also what an
	// instance that could not reach GitHub answers — see Checked.
	Available *Available
	// Checked says the lookup happened. False with a nil Available is
	// "could not ask", which is a different thing to say than "you are
	// up to date".
	Checked bool
	// Run is the update in progress, or the last one.
	Run *Run
}

// Check is what this instance is on and what it could move to.
//
// A **member's**, like the release notes: what version the software
// everyone is using is on is not a fact about the instance's
// configuration. Starting one is an admin's — see Start.
func (s *Service) Check(ctx context.Context, caller *user.User) (State, error) {
	if err := user.Require(caller, user.RoleMember); err != nil {
		return State{}, err
	}
	out := State{Version: s.version, Run: s.Current()}
	if s.version == "" {
		return out, nil
	}
	newer, err := Newer(ctx, s.client, s.version)
	if err != nil {
		// Not an error to the caller: an instance that cannot reach
		// GitHub is a normal instance, and a red banner about it on
		// every page is a red banner about somebody's firewall.
		log.Printf("update: checking for a newer release: %v", err)
		return out, nil
	}
	out.Checked, out.Available = true, newer
	return out, nil
}

// Start replaces this instance with a version.
//
// It returns as soon as the run is recorded and the work has started:
// what follows is the daemon replacing itself, and nothing can hold a
// connection open across that.
//
// **The order is the whole design.** The machines go first, because
// once this one restarts it can tell nobody anything; this one goes
// last, and hands its own replacement to a container that outlives it.
func (s *Service) Start(ctx context.Context, caller *user.User, version string) (*Run, error) {
	if err := user.Require(caller, RoleToUpdate); err != nil {
		return nil, err
	}
	if s.daemon == "" || s.daemonImage == "" {
		return nil, ErrNotAContainer
	}
	if s.version == "" {
		return nil, ErrNotAContainer
	}
	if r := s.Current(); r.Running() {
		return nil, ErrInProgress
	}

	want, err := Find(ctx, s.client, version)
	if err != nil {
		return nil, err
	}
	if release.Compare(want.Version, s.version) <= 0 {
		return nil, ErrNotNewer
	}

	if !s.running.TryLock() {
		return nil, ErrInProgress
	}
	run := &Run{
		Version: want.Version, From: s.version,
		Status: StatusRunning, StartedAt: time.Now(),
	}
	if err := s.store.Write(run); err != nil {
		s.running.Unlock()
		return nil, err
	}

	go func() {
		defer s.running.Unlock()
		// Its own context: the request that asked is long gone, and
		// what this is doing outlives every connection anyway.
		ctx, cancel := context.WithTimeout(context.Background(), StuckAfter)
		defer cancel()
		if err := s.run(ctx, run); err != nil {
			log.Printf("update to %s: %v", run.Version, err)
			s.store.Finish(run, err)
		}
	}()
	return run, nil
}

// run does the update. It returns nil when the last step has been
// handed to the container that replaces this one — there is nothing
// after that to report, because this process is about to stop existing.
func (s *Service) run(ctx context.Context, r *Run) error {
	if err := s.updateWorkers(ctx, r); err != nil {
		return err
	}

	s.store.Step(r, "pulling the new images")
	for _, ref := range []string{
		retag(s.daemonImage, r.Version),
		retag(s.frontendImage, r.Version),
	} {
		if ref == "" {
			continue
		}
		if err := s.docker.PullImage(ctx, ref, nil); err != nil {
			return fmt.Errorf("pull %s: %w", ref, err)
		}
	}

	// The dashboard first, and this process is still here to see it
	// fail. The daemon recreates it at boot anyway, so doing it now
	// costs nothing and buys one more thing that has already worked
	// before the irreversible step.
	s.store.Step(r, "replacing the dashboard")
	if err := s.replace(ctx, s.frontend, retag(s.frontendImage, r.Version)); err != nil {
		return fmt.Errorf("replace the dashboard: %w", err)
	}

	// And the last one, which this process cannot do itself.
	s.store.Step(r, "replacing the daemon")
	return s.handOver(ctx, r)
}

// updateWorkers tells every other machine to replace itself, and waits
// until each reports the new version.
//
// **Before this machine**, because once it restarts it can tell nobody
// anything: a control plane that updated first would leave a cluster
// where every worker is a release behind and nothing is coming to move
// them.
//
// A machine that does not come back is not a reason to stop. It is
// already `unreachable` on every screen, and refusing to update the
// instance because one box is off would make one dead machine a freeze
// on the whole cluster.
func (s *Service) updateWorkers(ctx context.Context, r *Run) error {
	if s.cluster == nil {
		return nil
	}
	workers, err := s.cluster.Workers(ctx)
	if err != nil {
		return err
	}
	behind := map[int64]bool{}
	for id, v := range workers {
		if release.Normalize(v) != r.Version {
			behind[id] = true
		}
	}
	if len(behind) == 0 {
		return nil
	}

	s.store.Step(r, fmt.Sprintf("updating %d other machine(s)", len(behind)))
	for id := range behind {
		if err := s.cluster.Update(ctx, id, r.Version); err != nil {
			log.Printf("update: telling machine %d: %v", id, err)
		}
	}

	deadline := time.Now().Add(WorkerTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(WorkerPoll):
		}
		workers, err := s.cluster.Workers(ctx)
		if err != nil {
			continue
		}
		left := 0
		for id := range behind {
			if release.Normalize(workers[id]) != r.Version {
				left++
			}
		}
		if left == 0 {
			return nil
		}
		s.store.Step(r, fmt.Sprintf("waiting for %d machine(s) to come back", left))
	}
	// Out of patience, and carrying on anyway. A machine that is off
	// gets the new version whenever it returns: its own agent is told
	// what to run, and being a release behind is a thing this instance
	// reports rather than a thing that stops it.
	s.store.Step(r, "carrying on without the machines that did not answer")
	return nil
}

// WorkerTimeout is how long the other machines are waited for, and
// WorkerPoll is how often they are asked. A machine replacing itself is
// down for as long as a pull takes, so the wait is generous and the
// poll is not.
const (
	WorkerTimeout = 10 * time.Minute
	WorkerPoll    = 5 * time.Second
)

// replace recreates a container from the same options with a new image.
func (s *Service) replace(ctx context.Context, name, image string) error {
	if name == "" || image == "" {
		return nil
	}
	spec, err := s.docker.SpecOf(ctx, name)
	if err != nil {
		return err
	}
	spec.Image = image
	if err := s.docker.StopContainer(ctx, name); err != nil {
		log.Printf("update: stopping %s: %v", name, err)
	}
	if err := s.docker.RemoveContainer(ctx, name); err != nil {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	id, err := s.docker.CreateContainer(ctx, spec)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	return s.docker.StartContainer(ctx, id)
}

// handOver starts the container that replaces this one.
//
// It runs the **new** image, with the Docker socket and the data
// directory, and does exactly one thing: recreate the daemon's
// container from its own options with the new image. It has to be a
// separate container because the process it stops is this one.
//
// AutoRemove, so a box is not left with a stopped updater on it; and no
// restart policy, because a one-shot that came back after a reboot
// would replace the daemon a second time.
func (s *Service) handOver(ctx context.Context, r *Run) error {
	spec, err := s.docker.SpecOf(ctx, s.daemon)
	if err != nil {
		return err
	}
	image := retag(s.daemonImage, r.Version)
	id, err := s.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:  s.daemon + "-updater",
		Image: image,
		Cmd:   []string{"-replace", s.daemon, "-replace-image", image},
		Binds: append([]string{"/var/run/docker.sock:/var/run/docker.sock"},
			s.store.DataDir+":"+s.store.DataDir),
		Env:        []string{"CUBESHIP_DATA_DIR=" + s.store.DataDir},
		Network:    spec.Network,
		AutoRemove: true,
	})
	if err != nil {
		return fmt.Errorf("create the updater: %w", err)
	}
	if err := s.docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start the updater: %w", err)
	}
	return nil
}

// Settle closes a run that this build is the end of.
//
// Called at startup: a daemon that comes up on the version a running
// record was moving to **is** that record finishing, and nothing else
// is left to say so — the updater that started it has gone, and it went
// before this process existed.
func (s *Service) Settle() {
	r := s.store.Read()
	if !r.Running() {
		return
	}
	if s.version != "" && r.Version == s.version {
		s.store.Finish(r, nil)
		log.Printf("update: this instance is now %s", s.version)
		return
	}
	if r.Stale(StuckAfter) {
		s.store.Finish(r, fmt.Errorf("the update did not finish, and this daemon came back on %s", s.version))
	}
}

// Retag swaps the tag on an image reference. Exported for the worker
// agent, which does the same thing to its own image and should not have
// a second opinion about where a tag ends.
func Retag(ref, version string) string { return retag(ref, version) }

// retag swaps the tag on an image reference.
//
// The reference is what the daemon was told — `install.sh` passes it,
// and an operator is free to point it at a mirror — so what changes is
// only ever the tag after the last colon, and only when that colon is
// not part of a port in the host.
func retag(ref, version string) string {
	if ref == "" {
		return ""
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref + ":" + version
}

// RoleToUpdate is what starting one takes. An admin's: it replaces
// every container on every machine in the cluster, and there is no
// undoing it from here.
const RoleToUpdate = user.RoleAdmin
