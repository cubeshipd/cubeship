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
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/firewall"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/node"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/dockerx"
)

// ContainerPrefix is what every container this instance creates is
// named under, on any machine in the cluster.
const ContainerPrefix = "cubeship-"

// dialTimeout bounds one pass. Well inside the interval, so a control
// plane that has stopped answering costs one skipped pass rather than a
// loop that stops calling.
const dialTimeout = 20 * time.Second

// Containers is what the agent counts on its own machine.
// *dockerx.Client satisfies it; a test supplies a fake.
type Containers interface {
	RunningNames(ctx context.Context) ([]string, error)
}

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
	Containers
	mesh.Engine

	PullImage(ctx context.Context, ref string, auth *dockerx.RegistryAuth) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
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

func New(controlPlane, token, version string, box *machine.Reader, engine Engine,
	address HostAddress, host firewall.Host,
) *Agent {
	return &Agent{
		controlPlane: strings.TrimRight(controlPlane, "/"),
		token:        token,
		version:      version,
		machine:      box,
		engine:       engine,
		address:      address,
		firewall:     host,
		client:       &http.Client{Timeout: dialTimeout},
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

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	answer, err := a.reconcile(ctx)
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
	// What the machine was told about the cluster's network, applied
	// here rather than reported on: an agent that knows how to join and
	// waits to be asked again would be a second round trip for an
	// instruction it already has.
	if answer.Mesh != nil {
		a.applyMesh(ctx, *answer.Mesh)
	}
	// What this machine is supposed to be running. Applied after the
	// network, because a container that comes up before the machine is
	// on the mesh is one that cannot reach the database it was given
	// the address of.
	a.pending = a.apply(ctx, answer.Desired.Apps, answer.Registry)
	return time.Duration(answer.IntervalSeconds) * time.Second, nil
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
	wanted := make(map[string]string, len(placements))
	for _, p := range placements {
		wanted[p.App] = p.Container
		if named(running, p.Container) != "" {
			// Already running it. Not news, and reporting it every ten
			// seconds would mark one deploy succeeded forever.
			continue
		}
		id, err := a.start(ctx, p, registry)
		result := node.Result{App: p.App, Deploy: p.Deploy, Container: id}
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
		want, placed := wanted[app]
		if placed && want == c.Name {
			continue
		}
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
	})
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	if err := a.engine.StartContainer(ctx, id); err != nil {
		a.discard(ctx, id)
		return "", fmt.Errorf("start container: %w", err)
	}
	for range healthAttempts {
		select {
		case <-ctx.Done():
			a.discard(ctx, id)
			return "", ctx.Err()
		case <-time.After(healthInterval):
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
	out := node.AgentRequest{Version: a.version, Cores: a.machine.Cores()}

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
	out.Containers = a.ours(ctx)
	out.Results = a.pending
	return out
}

// ours is how many of this instance's containers are running on this
// machine.
//
// By name, because that is what makes a container ours — and **not
// counting the daemon itself**: the question is what this instance put
// here, and the agent answering it is not that. An empty worker reports
// none rather than one, which is the true answer and the readable one.
func (a *Agent) ours(ctx context.Context) int {
	if a.engine == nil {
		return 0
	}
	names, err := a.engine.RunningNames(ctx)
	if err != nil {
		return 0
	}
	count := 0
	for _, name := range names {
		if name == bootstrap.DaemonContainerName {
			continue
		}
		if strings.HasPrefix(name, ContainerPrefix) {
			count++
		}
	}
	return count
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
