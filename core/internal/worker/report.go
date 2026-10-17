package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/machine"
	"cubeship/internal/mesh"
	"cubeship/internal/metrics"
	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
)

// start runs one placement: pull, create, start, and watch it long
// enough to know it did not exit on its own configuration.
//
// A container that will not come up is removed rather than left behind.
// The control plane is told why, and that is what the deployment's row
// says — so a failure here reads on the app's screen exactly like a
// failure on the control plane does.
func (a *Agent) start(ctx context.Context, p node.Placement, registry string, running []dockerx.Running) (string, string, []dockerx.Running, error) {
	auth := p.Registry
	if auth == nil && registry != "" && strings.HasPrefix(p.Image, registry+"/") {
		// The instance's own registry. Its credential is not in the
		// placement and could not be — the control plane holds only the
		// hash of it — so this machine authenticates as itself, with
		// the credential it already dials home with.
		auth = &dockerx.RegistryAuth{Username: node.RegistryUsername, Password: a.token}
	}
	if err := a.engine.PullImage(ctx, p.Image, auth); err != nil {
		return "", "", nil, fmt.Errorf("pull %s: %w", p.Image, err)
	}
	var output string
	if len(p.PredeployArgs) > 0 || p.PredeployShell != "" {
		runner, ok := a.engine.(interface {
			RunOneShot(context.Context, dockerx.ContainerOpts) (string, int, error)
		})
		if !ok {
			return "", "", nil, fmt.Errorf("pre-deploy command is configured but one-shot containers are unavailable")
		}
		hookTimeout := time.Duration(p.PredeployTimeout) * time.Second
		if hookTimeout == 0 {
			hookTimeout = 10 * time.Minute
		}
		hookCtx, cancel := context.WithTimeout(ctx, hookTimeout)
		opts := dockerx.ContainerOpts{Image: p.Image, Env: envvar.Slice(p.Env), CapDrop: []string{"NET_RAW"}, SecurityOpt: []string{"no-new-privileges:true"}, Resources: p.Resources}
		if len(p.Networks) > 0 {
			opts.Network, opts.AlsoNetworks = p.Networks[0], p.Networks[1:]
		}
		if p.PredeployShell != "" {
			opts.Entrypoint = []string{"sh"}
			opts.Cmd = []string{"-c", p.PredeployShell}
		} else {
			opts.Entrypoint = nil
			opts.Cmd = append([]string(nil), p.PredeployArgs...)
		}
		var code int
		var runErr error
		output, code, runErr = runner.RunOneShot(hookCtx, opts)
		output = redactOutput(output, p.Env)
		timedOut := errors.Is(hookCtx.Err(), context.DeadlineExceeded)
		cancel()
		if runErr != nil {
			if timedOut {
				return "", output, nil, fmt.Errorf("pre-deploy command timed out after %s", hookTimeout)
			}
			return "", output, nil, fmt.Errorf("run pre-deploy command: %w", runErr)
		}
		if code != 0 {
			return "", output, nil, fmt.Errorf("pre-deploy command exited with status %d: %s", code, strings.TrimSpace(output))
		}
	}
	displaced := a.displace(ctx, running, p)

	network, also := "", []string(nil)
	if len(p.Networks) > 0 {
		network, also = p.Networks[0], p.Networks[1:]
	}
	binds := make([]string, 0, len(p.Volumes))
	for _, v := range p.Volumes {
		dir := filepath.Join(a.dataDir, "volumes", strconv.FormatInt(v.ID, 10))
		if err := dockerx.SeedVolume(ctx, a.engine, dir, p.Image, v.Path); err != nil {
			return "", "", displaced, err
		}
		binds = append(binds, dir+":"+v.Path)
	}
	id, err := a.engine.CreateContainer(ctx, dockerx.ContainerOpts{
		Binds:        binds,
		Name:         p.Container,
		Image:        p.Image,
		Labels:       p.Labels,
		Env:          envvar.Slice(p.Env),
		Network:      network,
		CapDrop:      []string{"NET_RAW"},
		SecurityOpt:  []string{"no-new-privileges:true"},
		AlsoNetworks: also,
		Aliases:      p.Aliases,
		Resources:    p.Resources,
	})
	if err != nil {
		return "", output, displaced, fmt.Errorf("create container: %w", err)
	}
	if err := a.engine.StartContainer(ctx, id); err != nil {
		a.discard(ctx, id)
		return "", output, displaced, fmt.Errorf("start container: %w", err)
	}
	for range a.healthAttempts {
		select {
		case <-ctx.Done():
			a.discard(ctx, id)
			return "", output, displaced, ctx.Err()
		case <-time.After(a.healthInterval):
		}
		up, err := a.engine.IsRunning(ctx, id)
		if err != nil || !up {
			a.discard(ctx, id)
			return "", output, displaced, fmt.Errorf("it started and did not stay up")
		}
	}
	return id, output, displaced, nil
}

func redactOutput(output string, env map[string]string) string {
	for _, value := range env {
		if value == "" {
			continue
		}
		output = strings.ReplaceAll(output, value, "[REDACTED]")
	}
	return output
}

// discard removes a container that should not exist. Best effort: what
// went wrong is already the error being returned, and a container that
// cannot be removed is a line in a log rather than a second failure.
// displace stops the containers a copy with a volume is replacing, before
// its replacement starts: two containers on one data directory corrupt it.
// Every other copy starts first and retires the old one after, which is
// the second half of apply.
func (a *Agent) displace(ctx context.Context, running []dockerx.Running, p node.Placement) []dockerx.Running {
	if len(p.Volumes) == 0 {
		return nil
	}
	var stopped []dockerx.Running
	for _, c := range running {
		if c.Labels[node.LabelApp] != p.App || c.Name == p.Container ||
			node.OrdinalFromLabels(c.Labels) != node.OrdinalOf(p.Ordinal) {
			continue
		}
		if err := stopWithPolicy(ctx, a.engine, c.ID, p.StopGracePeriod, p.StopSignal); err != nil {
			log.Printf("agent: stopping %s before its replacement: %v", c.Name, err)
		}
		stopped = append(stopped, c)
	}
	return stopped
}

// settleDisplaced removes what displace stopped once the replacement is
// up, or starts it again when the replacement did not come up.
func (a *Agent) settleDisplaced(ctx context.Context, stopped []dockerx.Running, failed error) {
	for _, c := range stopped {
		if failed != nil {
			if err := a.engine.StartContainer(ctx, c.ID); err != nil {
				log.Printf("agent: starting %s again after its replacement failed: %v", c.Name, err)
			}
			continue
		}
		if err := a.engine.RemoveContainer(ctx, c.ID); err != nil {
			log.Printf("agent: removing %s: %v", c.Name, err)
		}
	}
}

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
	if a.hostSampler == nil {
		a.hostSampler = machine.NewCollector(nil, a.machine)
	}
	if a.hostReport == nil || time.Since(a.hostSampleAt) >= metrics.Interval {
		report := a.hostSampler.Telemetry()
		a.hostReport = &report
		a.hostSampleAt = time.Now()
	}
	out.Host = a.hostReport
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
		startedAt, _ := a.engine.ContainerStartedAt(ctx, c.ID)
		out = append(out, node.Reading{
			Container:        c.ID,
			StartedAt:        startedAt,
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
