package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/bootstrap"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/update"
)

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

	// Whatever is there under this name goes first: an updater that
	// outlived its own exit is a name already in use, and every update
	// from then on would fail on the create below.
	if err := a.engine.RemoveContainer(ctx, bootstrap.DaemonContainerName+"-updater"); err != nil {
		log.Printf("agent: clearing the previous updater: %v", err)
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
	allContainers, err := a.engine.AllContainers(ctx)
	if err != nil {
		log.Printf("agent: listing containers for cleanup: %v", err)
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
	policy := make(map[string]node.Placement, len(placements))
	for _, p := range placements {
		wanted[p.Container] = true
		byCopy[copy{app: p.App, ordinal: node.OrdinalOf(p.Ordinal)}] = p.Container
		policy[p.App] = p
	}

	for _, p := range placements {
		if a.isPaused(p.App) {
			// Stopped on purpose for a volume job, which starts it again.
			continue
		}
		if p.Stopped {
			if id := named(running, p.Container); id != "" {
				if err := stopWithPolicy(ctx, a.engine, id, p.StopGracePeriod, p.StopSignal); err != nil {
					log.Printf("agent: stopping %s: %v", p.Container, err)
				}
			}
			continue
		}
		if id := named(running, p.Container); id != "" {
			if p.ContainerID != "" && id == p.ContainerID {
				results = append(results, node.Result{App: p.App, Deploy: p.Deploy, Ordinal: p.Ordinal, Container: id, Resumed: true})
				continue
			}
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
		if p.ContainerID != "" {
			err := a.startExisting(ctx, p.ContainerID)
			result := node.Result{App: p.App, Deploy: p.Deploy, Ordinal: p.Ordinal, Resumed: true}
			if err != nil {
				result.Error = err.Error()
			} else {
				result.Container = p.ContainerID
			}
			results = append(results, result)
			continue
		}
		id, output, displaced, err := a.start(ctx, p, registry, running)
		a.settleDisplaced(ctx, displaced, err)
		// The ordinal goes back untouched. It is how the control plane
		// knows which copy of this app on this machine the result is
		// about, and without it every report is about a copy nothing
		// asked for.
		result := node.Result{App: p.App, Deploy: p.Deploy, Ordinal: p.Ordinal, Container: id, Output: output}
		// start currently returns only an error, so successful hook output is
		// carried separately by the placement result for central persistence.
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
		if refreshed, err := a.engine.AllContainers(ctx); err == nil {
			allContainers = refreshed
		}
	}

	for _, c := range allContainers {
		app := c.Labels[node.LabelApp]
		if app == "" {
			// Not a container this instance placed here. The daemon
			// itself, a database, something somebody ran by hand —
			// none of it is this loop's to touch.
			continue
		}
		if wanted[c.Name] || a.isPaused(app) {
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
		pol := policy[app]
		if c.Running {
			if err := stopWithPolicy(ctx, a.engine, c.ID, pol.StopGracePeriod, pol.StopSignal); err != nil {
				log.Printf("agent: stopping %s: %v", c.Name, err)
			}
		}
		if err := a.engine.RemoveContainer(ctx, c.ID); err != nil {
			log.Printf("agent: removing %s: %v", c.Name, err)
		}
	}
	return results
}

func (a *Agent) startExisting(ctx context.Context, id string) error {
	if err := a.engine.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start stopped container: %w", err)
	}
	for range a.healthAttempts {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.healthInterval):
		}
		up, err := a.engine.IsRunning(ctx, id)
		if err == nil && up {
			return nil
		}
	}
	return errors.New("container did not stay up")
}

// copy names one copy of one app on this machine: an app running three
// of itself here is three of these, and they are what a container is
// replaced against.
type copy struct {
	app     string
	ordinal int
}
