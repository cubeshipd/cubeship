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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/firewall"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/metrics"
	"cubeship/internal/node"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/update"
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
const workTimeout = 15 * time.Minute

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
	Logs(ctx context.Context, id, tail string) (io.ReadCloser, error)
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	SetResources(ctx context.Context, id string, r dockerx.Resources) error
	SpecOf(ctx context.Context, name string) (dockerx.ContainerOpts, error)
	IsRunning(ctx context.Context, id string) (bool, error)
	RunningContainers(ctx context.Context) ([]dockerx.Running, error)
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

	machine *machine.Reader
	engine  Engine
	address HostAddress
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

// replaceSelf pulls a version and hands this machine's own container to
// a throwaway container that replaces it.
//
// The same shape as the control plane's own update, and for the same
// reason: the process doing the work is the process being stopped. What
// differs is that there is nothing to report — a worker has no
// database, no status file anybody reads, and no screen. It comes back
// on the new version or it does not, and the control plane is watching
// which.
//
// **The app containers are not touched.** Replacing the agent is
// replacing the thing that decides; what it decided is already running,
// and it is still running while this happens.
func (a *Agent) replaceSelf(ctx context.Context, version string) error {
	if a.engine == nil {
		return fmt.Errorf("this server has no Docker")
	}
	if version == "" {
		return fmt.Errorf("no version to replace this machine with")
	}
	spec, err := a.engine.SpecOf(ctx, bootstrap.DaemonContainerName)
	if err != nil {
		return fmt.Errorf("read this machine's own settings: %w", err)
	}
	image := update.Retag(spec.Image, version)
	if err := a.engine.PullImage(ctx, image, nil); err != nil {
		return fmt.Errorf("pull %s: %w", image, err)
	}

	id, err := a.engine.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:  bootstrap.DaemonContainerName + "-updater",
		Image: image,
		Cmd:   []string{"-replace", bootstrap.DaemonContainerName, "-replace-image", image},
		Binds: []string{"/var/run/docker.sock:/var/run/docker.sock"},
		// No network of its own: it talks to the Engine over the
		// socket and to nothing else.
		AutoRemove: true,
	})
	if err != nil {
		return fmt.Errorf("create the updater: %w", err)
	}
	log.Printf("agent: replacing this machine with %s", image)
	return a.engine.StartContainer(ctx, id)
}

// readLog is the tail of a container's log, exactly as the Engine gives
// it: stdout and stderr multiplexed behind an 8-byte frame header per
// chunk.
//
// **Not demultiplexed here**, deliberately. The control plane already
// does that to every log it serves, and doing it on this side as well
// would mean two places that know Docker's frame format and one of them
// deciding, per app, which had already happened. What crosses the wire
// is what the Engine said; what a person sees is decided in one place.
func (a *Agent) readLog(ctx context.Context, cmd node.Command) ([]byte, error) {
	if a.engine == nil {
		return nil, fmt.Errorf("this server has no Docker to read a log from")
	}
	rc, err := a.engine.Logs(ctx, cmd.Container, cmd.Tail)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, node.MaxAnswerBytes))
}

// post sends one command's answer back. The body is the answer as
// bytes; a failure travels as a query parameter with no body, because
// there is nothing to send.
func (a *Agent) post(ctx context.Context, id string, output []byte, failed error) error {
	url := a.controlPlane + "/api/nodes/agent/results/" + id
	var body io.Reader
	if failed != nil {
		url += "?error=" + neturl.QueryEscape(failed.Error())
	} else {
		body = bytes.NewReader(output)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer "+a.token)

	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("control plane answered %d", res.StatusCode)
	}
	return nil
}

// apply makes this machine run what it was told to run, and reports
// what changed.
//
// Two halves, in this order. **Start what is missing**, then **remove
// what is not wanted** — the reverse would take an app down and then
// find out its replacement will not start. A container whose
// replacement is not up yet is left exactly where it is, which is what
// makes a failed deploy a no-op rather than an outage.
func (a *Agent) apply(ctx context.Context, placements []node.Placement, registry string) []node.Result {
	if a.engine == nil {
		return nil
	}
	running, err := a.engine.RunningContainers(ctx)
	if err != nil {
		log.Printf("agent: listing what is running: %v", err)
		return nil
	}

	var results []node.Result
	// What should be running here, twice over: by container name, which
	// is what says "keep this one", and by which copy of which app each
	// is, which is what says whether an old one may go.
	//
	// **Not one entry per app.** A machine running several copies of one
	// app has several containers, and a map keyed by the app holds only
	// the last of them — so every other copy read as unwanted, was
	// removed, was started again on the next pass, and flapped for the
	// life of the instance.
	wanted := make(map[string]bool, len(placements))
	byCopy := make(map[copy]string, len(placements))
	for _, p := range placements {
		wanted[p.Container] = true
		byCopy[copy{app: p.App, ordinal: node.OrdinalOf(p.Ordinal)}] = p.Container
	}

	for _, p := range placements {
		if id := named(running, p.Container); id != "" {
			// Already running it. Not news, and reporting it every ten
			// seconds would mark one deploy succeeded forever.
			//
			// Its ceiling is applied anyway, and every pass: it is the
			// one setting the Engine writes to a running container, so
			// this is what makes a limit raised on the control plane
			// take effect here without a deploy. Skipped when there is
			// no ceiling, which is the overwhelming majority of
			// containers — and it could not lift one either way, since
			// the Engine reads a zero as "leave that alone".
			if !p.Resources.Unlimited() {
				if err := a.engine.SetResources(ctx, id, p.Resources); err != nil {
					log.Printf("agent: %s: applying its ceiling: %v", p.App, err)
				}
			}
			continue
		}
		id, err := a.start(ctx, p, registry)
		// The ordinal goes back untouched. It is how the control plane
		// knows which copy of this app on this machine the result is
		// about, and without it every report is about a copy nothing
		// asked for.
		result := node.Result{App: p.App, Deploy: p.Deploy, Ordinal: p.Ordinal, Container: id}
		if err != nil {
			log.Printf("agent: %s: %v", p.App, err)
			result.Error = err.Error()
		} else {
			log.Printf("agent: %s is running as %s", p.App, p.Container)
		}
		results = append(results, result)
	}

	// Something started, so what is running has changed under the list
	// read above — and the next half decides what to remove by it.
	if len(results) > 0 {
		if refreshed, err := a.engine.RunningContainers(ctx); err == nil {
			running = refreshed
		}
	}

	for _, c := range running {
		app := c.Labels[node.LabelApp]
		if app == "" {
			// Not a container this instance placed here. The daemon
			// itself, a database, something somebody ran by hand —
			// none of it is this loop's to touch.
			continue
		}
		if wanted[c.Name] {
			continue
		}
		// Which copy this is, so what may replace it is **its own**
		// replacement rather than any container of the same app. An
		// app with no entry here at all is one this machine no longer
		// runs, and a copy with none is one that has been scaled away.
		want, placed := byCopy[copy{app: app, ordinal: node.OrdinalFromLabels(c.Labels)}]
		if placed && named(running, want) == "" {
			// Its replacement is not up. Leaving the old one running is
			// the whole point of doing this second.
			continue
		}
		log.Printf("agent: removing %s, which this instance no longer runs here", c.Name)
		if err := a.engine.StopContainer(ctx, c.ID); err != nil {
			log.Printf("agent: stopping %s: %v", c.Name, err)
		}
		if err := a.engine.RemoveContainer(ctx, c.ID); err != nil {
			log.Printf("agent: removing %s: %v", c.Name, err)
		}
	}
	return results
}

// copy names one copy of one app on this machine: an app running three
// of itself here is three of these, and they are what a container is
// replaced against.
type copy struct {
	app     string
	ordinal int
}

// start runs one placement: pull, create, start, and watch it long
// enough to know it did not exit on its own configuration.
//
// A container that will not come up is removed rather than left behind.
// The control plane is told why, and that is what the deployment's row
// says — so a failure here reads on the app's screen exactly like a
// failure on the control plane does.
func (a *Agent) start(ctx context.Context, p node.Placement, registry string) (string, error) {
	auth := p.Registry
	if auth == nil && registry != "" && strings.HasPrefix(p.Image, registry+"/") {
		// The instance's own registry. Its credential is not in the
		// placement and could not be — the control plane holds only the
		// hash of it — so this machine authenticates as itself, with
		// the credential it already dials home with.
		auth = &dockerx.RegistryAuth{Username: node.RegistryUsername, Password: a.token}
	}
	if err := a.engine.PullImage(ctx, p.Image, auth); err != nil {
		return "", fmt.Errorf("pull %s: %w", p.Image, err)
	}

	network, also := "", []string(nil)
	if len(p.Networks) > 0 {
		network, also = p.Networks[0], p.Networks[1:]
	}
	id, err := a.engine.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:         p.Container,
		Image:        p.Image,
		Labels:       p.Labels,
		Env:          envvar.Slice(p.Env),
		Network:      network,
		AlsoNetworks: also,
		Resources:    p.Resources,
	})
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	if err := a.engine.StartContainer(ctx, id); err != nil {
		a.discard(ctx, id)
		return "", fmt.Errorf("start container: %w", err)
	}
	for range a.healthAttempts {
		select {
		case <-ctx.Done():
			a.discard(ctx, id)
			return "", ctx.Err()
		case <-time.After(a.healthInterval):
		}
		up, err := a.engine.IsRunning(ctx, id)
		if err != nil || !up {
			a.discard(ctx, id)
			return "", fmt.Errorf("it started and did not stay up")
		}
	}
	return id, nil
}

// discard removes a container that should not exist. Best effort: what
// went wrong is already the error being returned, and a container that
// cannot be removed is a line in a log rather than a second failure.
func (a *Agent) discard(ctx context.Context, id string) {
	if err := a.engine.RemoveContainer(ctx, id); err != nil {
		log.Printf("agent: removing %s after it would not run: %v", id, err)
	}
}

// named finds a running container by name, and answers with its id.
func named(running []dockerx.Running, name string) string {
	for _, c := range running {
		if c.Name == name {
			return c.ID
		}
	}
	return ""
}

// applyMesh puts this machine on the cluster's private network.
//
// The firewall first and the swarm second, and that order is the whole
// of it: joining is an outbound call, which a firewall allows anyway,
// but the gossip and the traffic that follow are **inbound** from the
// peers. Joining before opening the ports is a machine that joins and
// then cannot be reached, which reads as a swarm that half worked.
//
// Failures are logged and not retried here: the next pass is ten
// seconds away and arrives with a fresh answer, which is a better retry
// than one that reasons about why the last one failed.
func (a *Agent) applyMesh(ctx context.Context, info mesh.Info) {
	if a.engine == nil {
		return
	}
	if err := a.admit(ctx, info.Peers); err != nil {
		log.Printf("agent: opening the cluster's ports: %v", err)
		return
	}
	if err := mesh.Join(ctx, a.engine, info, a.reported); err != nil {
		log.Printf("agent: joining the cluster's network: %v", err)
	}
}

// admit opens this machine's ports to its peers, when the set of them
// has changed. Each rule is a container through hostexec, so doing it
// on every pass would be a dozen throwaway containers a minute for a
// firewall that already says what it needs to.
func (a *Agent) admit(ctx context.Context, peers []string) error {
	sort.Strings(peers)
	key := strings.Join(peers, ",")
	if key == a.admitted {
		return nil
	}
	if err := mesh.Admit(ctx, a.firewall, peers, false); err != nil {
		return err
	}
	a.admitted = key
	return nil
}

// reconcile sends one report and returns what this machine is told.
func (a *Agent) reconcile(ctx context.Context) (node.AgentResponse, error) {
	body, err := json.Marshal(a.report(ctx))
	if err != nil {
		return node.AgentResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		a.controlPlane+"/api/nodes/agent/reconcile", bytes.NewReader(body))
	if err != nil {
		return node.AgentResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.token)

	res, err := a.client.Do(req)
	if err != nil {
		return node.AgentResponse{}, fmt.Errorf("reach the control plane at %s: %w", a.controlPlane, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// Errors are text/plain, which is what http.Error writes. The
		// status matters more than the words on the two that mean
		// something an operator has to act on.
		said, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		switch res.StatusCode {
		case http.StatusUnauthorized:
			return node.AgentResponse{}, fmt.Errorf(
				"the control plane does not recognise this machine's credential — it may have been removed from the cluster")
		default:
			return node.AgentResponse{}, fmt.Errorf("control plane answered %d: %s",
				res.StatusCode, strings.TrimSpace(string(said)))
		}
	}

	var answer node.AgentResponse
	if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
		return node.AgentResponse{}, fmt.Errorf("decode what the control plane said: %w", err)
	}
	return answer, nil
}

// report is what this machine says about itself.
//
// Every measurement is best-effort and independently so: a box whose
// disk cannot be read still reports its CPU. The control plane stores
// what arrives, and a zero it never overwrites is better than a pass
// dropped because one file was missing.
func (a *Agent) report(ctx context.Context) node.AgentRequest {
	// Wait is what turns a poll into a channel: with nothing to say the
	// control plane holds this request open, and the answer arrives
	// when something happens rather than when the next interval comes
	// round.
	out := node.AgentRequest{Version: a.version, Cores: a.machine.Cores(), Wait: true}

	if a.address != nil {
		out.Address = a.address.Address(ctx)
		// Kept, because it is what this machine advertises to the
		// others when it joins their swarm.
		a.reported = out.Address
	}
	if a.engine != nil {
		if state, err := a.engine.Swarm(ctx); err == nil && state.Active {
			out.MeshNodeID = state.NodeID
		}
	}
	if mem, err := a.machine.Memory(); err == nil {
		out.MemoryTotalBytes = mem.Total
		used := mem.Used
		out.MemoryBytes = &used
	}
	if disk, err := a.machine.Disk(); err == nil {
		out.DiskTotalBytes = disk.Total
		used := disk.Used
		out.DiskBytes = &used
	}
	if cpu, ok := a.cpu(); ok {
		out.CPUPercent = &cpu
	}
	out.Results = a.pending
	out.Readings = a.readings(ctx)
	return out
}

// readings are what the containers this machine runs are using, taken
// on the interval the control plane samples its own on.
//
// **The percentage is computed here**, with the same function the
// control plane uses, because it is a difference between two readings
// and this is the only place both of them exist. What crosses the wire
// is a number a chart can draw rather than counters somebody else has
// to hold state for.
//
// Nothing is taken until there is something to compare against: the
// first pass after this daemon starts records the counters and reports
// no percentage, which is the same rule the collector on the control
// plane follows.
func (a *Agent) readings(ctx context.Context) []node.Reading {
	if a.engine == nil || time.Since(a.sampledAt) < metrics.Interval {
		return nil
	}
	running, err := a.engine.RunningContainers(ctx)
	if err != nil {
		return nil
	}
	a.sampledAt = time.Now()

	previous := a.stats
	a.stats = map[string]dockerx.Stats{}

	var out []node.Reading
	for _, c := range running {
		// Only what this instance placed here. Everything else on the
		// box — the daemon itself, whatever somebody ran by hand — is
		// not something the control plane has a chart for.
		if c.Labels[node.LabelApp] == "" {
			continue
		}
		stats, err := a.engine.ContainerStats(ctx, c.ID)
		if err != nil {
			// A container that has just been removed is the common
			// case, and it is not worth a line every half minute.
			continue
		}
		a.stats[c.ID] = stats

		last, seen := previous[c.ID]
		if !seen {
			continue
		}
		out = append(out, node.Reading{
			Container:        c.ID,
			CPUPercent:       metrics.CPUPercent(last, stats),
			MemoryBytes:      int64(stats.MemoryBytes),
			MemoryLimitBytes: int64(stats.MemoryLimit),
		})
	}
	return out
}

func (a *Agent) cpu() (float64, bool) {
	current, err := a.machine.CPU()
	if err != nil {
		return 0, false
	}
	last := a.previous
	a.previous = &current
	if last == nil {
		return 0, false
	}
	total := current.Total - last.Total
	if current.Total < last.Total || total == 0 {
		return 0, false
	}
	percent := float64(current.Busy-last.Busy) / float64(total) * 100
	if percent < 0 || percent > 100 {
		return 0, false
	}
	return percent, true
}
