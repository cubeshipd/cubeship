// Package worker is the daemon running as somebody else's machine.
//
// A worker is the same binary as the control plane and almost none of
// the same program. It opens no database, serves no dashboard, runs no
// registry and no builder, and — the part worth saying out loud —
// **publishes no port at all**. Its entire network presence is an
// outbound call to the control plane every few seconds.
//
// That is why this is a package of its own rather than flags threaded
// through `server`: a worker is not the control plane with features
// turned off. It is a loop that says what this machine is and does what
// it is told, and building it as a mode of the other would have meant
// every module growing a branch for a case where none of them run.
//
// What it does with the answer is nothing yet — see node.Desired. The
// loop is here first on purpose: the way to reach a machine is the hard
// half, and it is worth having working and visible on a screen before
// anything is placed through it.
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"cubeship/internal/components"
	"cubeship/internal/firewall"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
)

// dialTimeout bounds one call home.
//
// Longer than the control plane holds a poll open, because that is what
// it is waiting through: a poll parked for its full wait and then
// answered is a normal pass, and a client that gave up at twenty
// seconds would have made this channel useless by cutting every quiet
// poll short.
const dialTimeout = node.PollWait + 20*time.Second

// workTimeout bounds what a pass *does* rather than what it waits for.
//
// Its own budget because the two are nothing alike: a call home is a
// small request, and starting a placement is an image pull, which on a
// slow box and a large image is minutes. Bounding both by the same
// number is how a pull gets killed for taking longer than a heartbeat.
const workTimeout = 65 * time.Minute

// HostAddress is how the agent works out where its machine is reached
// from outside. settings.RouteAddress satisfies it — the same door the
// control plane finds its own address through, which is the point: a
// worker's address is used for exactly what the control plane's is.
type HostAddress interface {
	Address(ctx context.Context) string
}

// Engine is what the agent needs from Docker: the cluster's swarm, and
// the containers it is told to run. *dockerx.Client satisfies it.
type Engine interface {
	mesh.Engine

	PullImage(ctx context.Context, ref string, auth *dockerx.RegistryAuth) error
	ContainerStats(ctx context.Context, id string) (dockerx.Stats, error)
	ContainerStartedAt(ctx context.Context, id string) (*time.Time, error)
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	SetResources(ctx context.Context, id string, r dockerx.Resources) error
	SpecOf(ctx context.Context, name string) (dockerx.ContainerOpts, error)
	IsRunning(ctx context.Context, id string) (bool, error)
	RunningContainers(ctx context.Context) ([]dockerx.Running, error)
	AllContainers(ctx context.Context) ([]dockerx.Running, error)
	PathOwner(ctx context.Context, image, path string) (dockerx.Owner, error)
}

type policyStopper interface {
	StopContainerWithOptions(context.Context, string, time.Duration, string) error
}

func stopWithPolicy(ctx context.Context, e Engine, id string, grace int64, signal string) error {
	if signal == "" {
		if grace == 0 {
			grace = 10
		}
		signal = "SIGTERM"
	}
	if s, ok := e.(policyStopper); ok {
		return s.StopContainerWithOptions(ctx, id, time.Duration(grace)*time.Second, signal)
	}
	return e.StopContainer(ctx, id)
}

// How long the agent watches a container it has just started before
// calling it up, and how often.
//
// Shorter than the control plane's own health check, and deliberately:
// what this is catching is an image that exits on its own configuration
// in the first seconds. A container that is still up after this is one
// the control plane's deployment row can be told about, and a container
// that dies later is what the next pass sees.
const (
	healthAttempts = 10
	healthInterval = time.Second
)

// How long this agent watches a container it just started, as fields
// rather than the constants above so a test does not have to wait ten
// real seconds per placement. Same shape the control plane's own
// orchestrator uses, and for the same reason.

// Agent is the loop.
type Agent struct {
	// controlPlane is the instance this machine belongs to, as a base
	// URL. The API lives under /api on it, like everywhere else.
	controlPlane string
	token        string
	version      string

	machine      *machine.Reader
	hostSampler  *machine.Collector
	hostReport   *machine.Telemetry
	hostSampleAt time.Time
	engine       Engine
	address      HostAddress
	// firewall is how this machine opens the cluster's ports to its
	// peers. Nil on a daemon with no way to reach the host, which is a
	// machine whose firewall is somebody else's business.
	firewall firewall.Host

	// healthAttempts and healthInterval are how long a container is
	// watched before it counts as up. Defaults from the constants
	// above; a test turns the interval off.
	healthAttempts int
	healthInterval time.Duration

	// dataDir is this machine's own state directory, and the one thing
	// the agent writes into: the routes its Traefik reads. Mounted at
	// the same path inside and out, like every other machine's.
	dataDir string

	// admitted is the peer set this machine's firewall was last opened
	// for. Each rule costs a container through hostexec, so it is
	// written when the cluster changes rather than on every pass.
	admitted string

	client *http.Client

	// previous is the CPU reading the last pass took, because a
	// percentage is a difference. The first pass reports none rather
	// than a zero — an invented first point is a point somebody reads
	// as a fact, which is the rule the metric collector follows too.
	previous *machine.CPUTime

	// reported is the address this machine last worked out for itself,
	// which is what it advertises to the swarm.
	reported string

	// stats is the last raw reading of each container this machine
	// runs, because a CPU percentage is a difference. Keyed by
	// container id rather than by app: a redeployed app is a new
	// container, and comparing across the swap would produce one
	// impossible reading.
	stats map[string]dockerx.Stats
	// sampledAt is when the last set of readings was taken. A machine
	// polls far more often than a chart wants a point.
	sampledAt time.Time

	// pending is what this machine did since its last pass, waiting to
	// be told to the control plane. Carried across a failed pass rather
	// than dropped: a deploy that ran here and could not be reported is
	// a deployment row that would sit `pending` forever.
	pending []node.Result

	// joined is whether the first successful pass has been logged.
	// Saying it once is what the installer waits for; saying it every
	// ten seconds would be a log nobody can read.
	joined bool

	// paused is the apps a volume job has stopped, by reference, counted.
	// apply leaves them alone until the job starts them again. Guarded by
	// jobs, because a job runs beside the loop.
	jobs   sync.Mutex
	paused map[string]int
	// s3 opens the bucket a volume job names.
	s3 s3Opener
}

func New(controlPlane, token, version, dataDir string, box *machine.Reader, engine Engine,
	address HostAddress, host firewall.Host,
) *Agent {
	return &Agent{
		controlPlane:   strings.TrimRight(controlPlane, "/"),
		token:          token,
		version:        version,
		dataDir:        dataDir,
		machine:        box,
		engine:         engine,
		address:        address,
		firewall:       host,
		healthAttempts: healthAttempts,
		healthInterval: healthInterval,
		client:         &http.Client{Timeout: dialTimeout},
		s3:             openS3,
	}
}

// Run calls home until ctx is done.
//
// The first pass is immediate: a machine somebody has just installed
// should appear on the cluster screen while they are still looking at
// it, not ten seconds later.
func (a *Agent) Run(ctx context.Context) {
	interval := node.Interval
	for {
		next, err := a.tick(ctx)
		if err == nil && next > 0 {
			// The control plane serves the cadence, so a cluster's
			// clock is its to change without upgrading every worker.
			interval = next
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// tick is one pass, and it recovers: this is the daemon's only
// goroutine on a worker, and a panic in it would take down a machine
// that is otherwise running somebody's apps perfectly well.
func (a *Agent) tick(ctx context.Context) (interval time.Duration, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("agent: pass panicked: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("panicked")
		}
	}()

	call, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	answer, err := a.reconcile(call)
	if err == nil {
		// Reported. Anything that happens from here is this pass's.
		a.pending = nil
	}
	if err != nil {
		// Every failure here is the same shape — the control plane is
		// not answering — and a worker whose network is down would
		// otherwise fill a disk with it. One line a pass, and the pass
		// is ten seconds.
		log.Printf("agent: %v", err)
		return 0, err
	}
	if !a.joined {
		a.joined = true
		// The line the installer waits for. It names the node this
		// machine joined as, because that is what somebody would
		// otherwise have to go to the dashboard to find out.
		log.Printf("agent: joined %s as %s", a.controlPlane, answer.Name)
	}
	// Everything below is work rather than waiting, and it gets its own
	// budget: an image pull is minutes, and the call home above is a
	// small request that must not share a deadline with one.
	work, stop := context.WithTimeout(ctx, workTimeout)
	defer stop()

	// What the machine was told about the cluster's network, applied
	// here rather than reported on: an agent that knows how to join and
	// waits to be asked again would be a second round trip for an
	// instruction it already has.
	if answer.Mesh != nil {
		a.applyMesh(work, *answer.Mesh)
	}
	// What this machine is supposed to be running. Applied after the
	// network, because a container that comes up before the machine is
	// on the mesh is one that cannot reach the database it was given
	// the address of.
	a.pending = a.apply(work, answer.Desired.Apps, answer.Registry)

	// And whatever this instance asked for while the poll was parked.
	// Answered one at a time and in order: there is one of each of
	// these in flight per screen somebody is looking at, not a queue.
	for _, cmd := range answer.Commands {
		a.answer(work, cmd)
	}

	// Something happened, so the control plane is told now rather than
	// after another wait. A deploy that finished and sat unreported for
	// half a minute is a screen that says `pending` for half a minute.
	if len(a.pending) > 0 {
		return time.Second, nil
	}
	return time.Duration(answer.IntervalSeconds) * time.Second, nil
}

// answer does one thing the control plane asked for and posts the
// result back.
//
// The result goes to its own endpoint rather than riding the next poll,
// because somebody is waiting on it: the request that asked is parked
// on the control plane until this lands.
func (a *Agent) answer(ctx context.Context, cmd node.Command) {
	var output []byte
	var failed error

	switch cmd.Kind {
	case components.CommandInventory:
		engine, _ := a.engine.(components.Engine)
		var inventory []components.Component
		inventory, failed = components.Inspect(ctx, engine, true)
		if failed == nil {
			output, failed = json.Marshal(inventory)
		}
	case components.CommandLogs:
		engine, _ := a.engine.(components.Engine)
		output, failed = components.ReadLogs(ctx, engine, cmd.Container, cmd.Tail)
	case node.CommandLogs:
		output, failed = a.readLog(ctx, cmd)
	case node.CommandUpdate:
		// **Nothing is posted back for this one**, on purpose: what it
		// starts is this container being stopped, so an answer would
		// be a message from a process that is about to be killed. The
		// control plane learns it worked from the version this machine
		// reports on the pass after it comes back.
		if err := a.replaceSelf(ctx, cmd.Version); err != nil {
			log.Printf("agent: replacing this machine with %s: %v", cmd.Version, err)
		}
		return
	case node.CommandShell:
		// Answered by connecting back, not by posting: a session is a
		// connection somebody is waiting on, and it lasts as long as
		// they type.
		go a.shell(cmd)
		return
	case node.CommandVolumeBackup, node.CommandVolumeRestore:
		// Answered by the job itself, when the copy is done.
		go a.volumeJob(cmd)
		return
	default:
		// A command this agent does not know is one from a control
		// plane newer than it. Saying so is better than silence: the
		// screen that asked gets a sentence rather than a timeout.
		failed = fmt.Errorf("this server's daemon does not know how to %q — it may be older than the control plane", cmd.Kind)
	}
	if err := a.post(ctx, cmd.ID, output, failed); err != nil {
		log.Printf("agent: answering %s: %v", cmd.Kind, err)
	}
}
