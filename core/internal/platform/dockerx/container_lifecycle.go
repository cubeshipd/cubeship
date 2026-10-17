package dockerx

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ContainerStats reads one sample for a container.
//
// One-shot, which returns immediately with no previous reading in it.
// The alternative — the Engine's `stream=false` — computes a delta for
// you by sleeping about a second first, which is a second per container
// on every collection pass, and produces a percentage averaged over
// that second rather than over the interval anybody is charting.
func (c *Client) ContainerStats(ctx context.Context, containerID string) (Stats, error) {
	resp, err := c.api.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return Stats{}, fmt.Errorf("stats for container %q: %w", containerID, err)
	}
	defer resp.Body.Close()

	var raw container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Stats{}, fmt.Errorf("decode stats for container %q: %w", containerID, err)
	}

	cpus := int(raw.CPUStats.OnlineCPUs)
	if cpus == 0 {
		// Older Engines report the per-CPU slice instead, and a machine
		// with no answer at all is treated as one core rather than
		// zero — a division by it is the alternative.
		cpus = len(raw.CPUStats.CPUUsage.PercpuUsage)
	}
	if cpus == 0 {
		cpus = 1
	}

	// What the kernel counts as used includes the page cache it would
	// hand back under pressure. `docker stats` subtracts it and so does
	// this, or every container looks as though it is about to run out.
	// The key is named differently under cgroup v1 and v2.
	used := raw.MemoryStats.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if cached, ok := raw.MemoryStats.Stats[key]; ok {
			if cached < used {
				used -= cached
			}
			break
		}
	}

	return Stats{
		CPUTotal:    raw.CPUStats.CPUUsage.TotalUsage,
		CPUSystem:   raw.CPUStats.SystemUsage,
		OnlineCPUs:  cpus,
		MemoryBytes: used,
		MemoryLimit: raw.MemoryStats.Limit,
	}, nil
}

// RunOneShot runs a container to completion and returns everything it
// printed, plus its exit code.
//
// Both streams together, deliberately: this exists for commands whose
// failure is the interesting part, and a tool that writes its refusal to
// stderr would otherwise come back as an exit code with no words
// attached.
//
// The container is removed either way. A one-shot left behind is
// rubbish that accumulates one row per invocation in `docker ps -a`.
func (c *Client) RunOneShot(ctx context.Context, opts ContainerOpts) (output string, exitCode int, err error) {
	id, err := c.CreateContainer(ctx, opts)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		// Best effort: the caller's answer is already in hand, and a
		// failed cleanup is not a reason to lose it.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.RemoveContainer(cleanup, id)
	}()

	// Started first, then waited on — and the order is the whole
	// correctness of this.
	//
	// It was the other way round, to avoid what looked like the obvious
	// race: a fast command exiting before the wait is registered. That
	// race does not exist — the Engine answers a wait on an
	// already-exited container immediately, with its real exit code.
	//
	// The one that does exist is the mirror image, and waiting first
	// walked straight into it: a container that has been *created* and
	// not yet started is already "not running", so the wait returned at
	// once, with exit code 0 and an empty log, before the command had
	// done anything at all. Then the deferred remove killed it
	// mid-flight. Every command reported success and had no effect —
	// reads came back empty and writes never landed, which is a very
	// long way from where anybody would start looking.
	if err := c.StartContainer(ctx, id); err != nil {
		return "", 0, err
	}
	waitCh, errCh := c.api.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return "", 0, fmt.Errorf("wait for %s: %w", opts.Name, err)
	case done := <-waitCh:
		exitCode = int(done.StatusCode)
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}

	logs, err := c.Logs(ctx, id, "all")
	if err != nil {
		return "", exitCode, err
	}
	defer logs.Close()
	body, err := io.ReadAll(io.LimitReader(logs, 256*1024))
	if err != nil {
		return "", exitCode, err
	}
	return string(demux(body)), exitCode, nil
}

// demux strips Docker's stream framing.
//
// A container without a TTY has its output framed: eight bytes in front
// of every chunk, the first saying which stream it came from and the
// last four the length. Handing that to a parser gives it a stray NUL
// and a length byte in the middle of a line — which, for `ufw status`,
// is a rule that does not parse for a reason nobody would guess.
func demux(b []byte) []byte {
	var out []byte
	for len(b) >= 8 {
		// The first byte is the stream id: 0, 1 or 2. Anything else
		// means this is not framed output after all, so the rest is
		// taken as written.
		if b[0] > 2 {
			return append(out, b...)
		}
		n := int(binary.BigEndian.Uint32(b[4:8]))
		b = b[8:]
		if n > len(b) {
			n = len(b)
		}
		out = append(out, b[:n]...)
		b = b[n:]
	}
	return append(out, b...)
}

// PublishedPort is a host port some container is answering on, and
// which one.
type PublishedPort struct {
	Port int
	// Inside is the port the container itself listens on, which is not
	// always the published one: a datastore is published on 15000-15999
	// and listens on 5432 or 6379.
	//
	// It matters for exactly one caller and it is the whole of a bug
	// that had to be found by reading iptables counters — a firewall
	// rule for forwarded traffic matches the port **after** Docker's
	// DNAT, which is this one. See internal/firewall.
	Inside    int
	Protocol  string
	Container string
}

// Running is one container this Engine has up, as the agent needs it:
// what it is called, what id it has, and what it was labelled with.
type Running struct {
	ID      string
	Name    string
	Labels  map[string]string
	Running bool
}

// RunningContainers is every container up on this Engine.
//
// Labels come with them because that is how a machine tells its own
// work apart from everything else on the box: an app placed here
// carries the reference it belongs to, and a container with no such
// label is not this instance's to reason about.
func (c *Client) RunningContainers(ctx context.Context) ([]Running, error) {
	list, err := c.api.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]Running, 0, len(list))
	for _, item := range list {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		out = append(out, Running{ID: item.ID, Name: name, Labels: item.Labels, Running: true})
	}
	return out, nil
}

// AllContainers includes stopped containers so the worker can remove one
// after its app is deleted; RunningContainers remains the start/reconcile view.
func (c *Client) AllContainers(ctx context.Context) ([]Running, error) {
	list, err := c.api.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]Running, 0, len(list))
	for _, item := range list {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		out = append(out, Running{ID: item.ID, Name: name, Labels: item.Labels, Running: item.State == "running"})
	}
	return out, nil
}

// RunningNames is the name of every container running on this Engine.
//
// Names rather than the Engine's own structures, because the one
// question anybody here asks of this is "how many of ours" — and what
// makes a container ours is its name. Deciding which of them count is
// the caller's; this only says what is there.
func (c *Client) RunningNames(ctx context.Context) ([]string, error) {
	list, err := c.api.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if len(item.Names) == 0 {
			continue
		}
		out = append(out, strings.TrimPrefix(item.Names[0], "/"))
	}
	return out, nil
}

// PublishedPorts is every host port a running container has published.
//
// It exists for the firewall, and for one reason: on a Docker host, the
// ports that are actually exposed to the internet are not the ones the
// host's own services listen on — they are these, and they are the ones
// a firewall over UFW alone does not govern. A screen that is about to
// start denying forwarded traffic has to be able to say what it is
// about to deny.
//
// Ports bound to loopback are left out. They are not reachable from
// anywhere a firewall rule would apply, and listing them would invite
// somebody to open a hole for something that was never exposed.
func (c *Client) PublishedPorts(ctx context.Context) ([]PublishedPort, error) {
	list, err := c.api.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	seen := map[string]bool{}
	var out []PublishedPort
	for _, item := range list {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		for _, p := range item.Ports {
			if p.PublicPort == 0 {
				continue
			}
			if p.IP == "127.0.0.1" || p.IP == "::1" {
				continue
			}
			// One container publishes a port once per address family,
			// and a rule is about the port.
			key := fmt.Sprintf("%d/%s", p.PublicPort, p.Type)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, PublishedPort{
				Port: int(p.PublicPort), Inside: int(p.PrivatePort),
				Protocol: p.Type, Container: name,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// ExecStream runs a command inside a running container and moves its
// bytes rather than collecting them: stdin from r, stdout to w.
//
// It exists beside Exec because the two are different jobs. Exec hands
// back a transcript and caps it, which is right for a maintenance
// command somebody reads. A database dump is gigabytes and is being
// written to a bucket as it is produced — holding it in memory first
// would make the largest database this instance can back up whatever it
// has left of RAM.
//
// **stderr is kept apart**, which Exec deliberately does not do. Mixed
// into stdout it would land in the middle of the dump, and a dump with
// `pg_dump: warning:` a third of the way through is one that restores
// into a syntax error. `Tty: false` is what makes the Engine frame the
// two streams; stdcopy is what separates them again.
//
// A nil r attaches no stdin at all. A command told to read one and given
// nothing waits for an EOF that never comes.
func (c *Client) ExecStream(ctx context.Context, containerID string, cmd []string, r io.Reader, w io.Writer) (stderr string, exitCode int, err error) {
	created, err := c.api.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  r != nil,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", 0, fmt.Errorf("create exec: %w", err)
	}

	attached, err := c.api.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("attach exec: %w", err)
	}
	defer attached.Close()

	// Written on its own goroutine, and the write half is closed after:
	// a restore's command reads until EOF, and nothing else produces
	// one. Errors here are left to the exit status — a command that
	// stopped reading is one that already failed, and reporting the
	// broken pipe instead of what it said would replace the reason with
	// its consequence.
	if r != nil {
		go func() {
			_, _ = io.Copy(attached.Conn, r)
			_ = attached.CloseWrite()
		}()
	}

	// Read to EOF before inspecting: the command has not finished until
	// its output stream closes, and asking sooner reports it running.
	var errs strings.Builder
	if _, err := stdcopy.StdCopy(w, &limited{w: &errs, left: maxExecOutput}, attached.Reader); err != nil {
		return errs.String(), 0, fmt.Errorf("read exec output: %w", err)
	}

	inspected, err := c.api.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return errs.String(), 0, fmt.Errorf("inspect exec: %w", err)
	}
	return errs.String(), inspected.ExitCode, nil
}

// limited is a writer that keeps the first n bytes and swallows the
// rest, for stderr: a command that fails per row explains itself in its
// first lines and then repeats itself for as long as the dump is long.
type limited struct {
	w    io.Writer
	left int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.left <= 0 {
		return len(p), nil
	}
	if len(p) > l.left {
		p = p[:l.left]
	}
	n, err := l.w.Write(p)
	l.left -= n
	return len(p), err
}
