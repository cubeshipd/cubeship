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
	"strings"
	"time"

	"cubeship/internal/machine"
	"cubeship/internal/node"
	"cubeship/internal/platform/bootstrap"
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

// Agent is the loop.
type Agent struct {
	// controlPlane is the instance this machine belongs to, as a base
	// URL. The API lives under /api on it, like everywhere else.
	controlPlane string
	token        string
	version      string

	machine    *machine.Reader
	containers Containers
	address    HostAddress

	client *http.Client

	// previous is the CPU reading the last pass took, because a
	// percentage is a difference. The first pass reports none rather
	// than a zero — an invented first point is a point somebody reads
	// as a fact, which is the rule the metric collector follows too.
	previous *machine.CPUTime

	// joined is whether the first successful pass has been logged.
	// Saying it once is what the installer waits for; saying it every
	// ten seconds would be a log nobody can read.
	joined bool
}

func New(controlPlane, token, version string, box *machine.Reader, containers Containers, address HostAddress) *Agent {
	return &Agent{
		controlPlane: strings.TrimRight(controlPlane, "/"),
		token:        token,
		version:      version,
		machine:      box,
		containers:   containers,
		address:      address,
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
	return time.Duration(answer.IntervalSeconds) * time.Second, nil
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
	if a.containers == nil {
		return 0
	}
	names, err := a.containers.RunningNames(ctx)
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
