package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"cubeship/internal/envvar"
	"cubeship/internal/platform/dockerx"
)

// swapInPlace replaces the copy of an app with a volume or a published TCP
// port: **the old container stops before the new one starts.** Two
// containers on one data directory is how a database or a queue corrupts
// itself, and a host port is bound by one container at a time, so this app
// is unavailable for the seconds between — the one deploy here that is not
// zero-downtime.
//
// A new container that will not come up is removed and the old one is
// started again, so a bad image costs the downtime and nothing else.
func (o *Orchestrator) swapInPlace(ctx context.Context, a *Scoped, replica Replica, image Image,
	env envvar.Map, labels map[string]string, base string, deploymentID int64,
) error {
	appName := ReferenceOf(a).String()
	newName := containerNameFor(base, deploymentID, replica.Ordinal)
	if replica.Container != "" && replica.Name == newName {
		return nil
	}
	if len(a.Volumes) > 0 && o.dataDir == "" {
		return ErrNoDataDir
	}
	binds := make([]string, 0, len(a.Volumes))
	for _, v := range a.Volumes {
		if err := dockerx.SeedVolume(ctx, o.docker, VolumeDir(o.dataDir, v.ID), image.Ref, v.Path); err != nil {
			return err
		}
		binds = append(binds, VolumeBind(o.dataDir, v.ID, v.Path))
	}

	old := replica.Container
	if old != "" {
		if err := o.stop(ctx, old, &a.App); err != nil {
			log.Printf("deploy %s: could not stop the previous container %s: %v", appName, old, err)
		}
	}
	restore := func() {
		if old == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := o.docker.StartContainer(cleanup, old); err != nil {
			log.Printf("deploy %s: could not start the previous container %s again: %v", appName, old, err)
		}
	}

	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:         newName,
		Image:        image.Ref,
		Labels:       labels,
		Env:          envvar.Slice(env),
		Network:      Network,
		CapDrop:      []string{"NET_RAW"},
		SecurityOpt:  []string{"no-new-privileges:true"},
		AlsoNetworks: o.mesh(ctx),
		Aliases:      []string{base},
		Resources:    a.Limits.Resources(),
		Binds:        binds,
		Ports:        tcpPortSpecs(a.TCPPorts),
	})
	if err != nil {
		restore()
		return fmt.Errorf("create container: %w", err)
	}
	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that would not start")
		restore()
		return fmt.Errorf("start container: %w", err)
	}
	if err := o.waitHealthy(ctx, newID, a.HealthPath, healthPort(a), a.HealthPath == "" && len(a.TCPPorts) > 0); err != nil {
		o.removeContainer(ctx, newID, "abandoning a container that never became healthy")
		restore()
		return fmt.Errorf("health check timed out for container %s: %w", newID, err)
	}
	if err := o.apps.UpdateContainer(ctx, a.ID, replica.NodeID, replica.Ordinal, newID, newName,
		deploymentID, StatusRunning); err != nil {
		o.removeContainer(ctx, newID, "rolling back a deploy the database did not record")
		restore()
		return fmt.Errorf("update app container: %w", err)
	}
	if old != "" && old != newID {
		o.removeContainer(ctx, old, "retiring the previous container")
	}
	return nil
}

// Logs returns the app's container log. tail limits it to that many
// trailing lines; an empty tail returns the whole log.
func (o *Orchestrator) Logs(ctx context.Context, appID int64, tail string) (io.ReadCloser, error) {
	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return nil, ErrNotFound
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return nil, err
	}
	// This machine's own replica, never whichever container the app has
	// somewhere. A container id from another Engine is one this Docker
	// answers "no such container" for, which reads as an app that is
	// down rather than as a log that is somewhere else.
	mine, ok := a.ReplicaOn(here)
	if !ok || mine.Container == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, mine.Container, tail)
}

// Retire stops and removes an app's container, if it has one. It is what
// deleting an app calls before the row goes: a container left running
// with no row would serve traffic that nothing knows how to stop.
func (o *Orchestrator) Retire(ctx context.Context, appID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	stopped := a.Status() == StatusStopped
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	// Only this machine's own containers, and every one of them. A
	// replica on another machine goes when that machine next asks what
	// it should be running and does not find this app in the answer —
	// the row is being deleted, so it will not be. Reaching for it here
	// would mean stopping a container through an Engine this daemon
	// cannot see.
	for _, mine := range a.ReplicasOn(here) {
		if mine.Container == "" {
			continue
		}
		if !stopped {
			if err := o.stop(ctx, mine.Container, a); err != nil {
				log.Printf("retiring app %d: could not stop container %s: %v", appID, mine.Container, err)
			}
		}
		// Unlike the log-and-continue cases in Deploy, this one is
		// returned: the caller is about to delete the row, and doing
		// that while a container survives is exactly the state to
		// avoid.
		if err := o.docker.RemoveContainer(ctx, mine.Container); err != nil {
			return err
		}
	}
	return nil
}

// StopApp and StartApp serialize manual lifecycle changes with deploys.
func (o *Orchestrator) StopApp(ctx context.Context, appID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()
	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	if a.Status() == StatusStopped {
		return nil
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	for _, replica := range a.ReplicasOn(here) {
		if replica.Container == "" {
			continue
		}
		if err := o.stop(ctx, replica.Container, a); err != nil {
			return fmt.Errorf("stop app container %s: %w", replica.Name, err)
		}
	}
	return o.apps.SetAppStatus(ctx, appID, StatusStopped)
}

func (o *Orchestrator) StartApp(ctx context.Context, appID int64) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()
	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	if a.Status() != StatusStopped {
		return nil
	}
	if err := o.apps.SetAppStatus(ctx, appID, StatusStarting); err != nil {
		return err
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		_ = o.apps.SetAppStatus(ctx, appID, StatusStopped)
		return err
	}
	var started []Replica
	rollback := func() {
		for _, replica := range started {
			if err := o.stop(ctx, replica.Container, a); err != nil {
				log.Printf("app %d: could not roll back started container %s: %v", appID, replica.Name, err)
			}
		}
		_ = o.apps.SetAppStatus(ctx, appID, StatusStopped)
	}
	for _, replica := range a.ReplicasOn(here) {
		if replica.Container == "" {
			continue
		}
		if err := o.docker.StartContainer(ctx, replica.Container); err != nil {
			rollback()
			return fmt.Errorf("start app container %s: %w", replica.Name, err)
		}
		started = append(started, replica)
		if err := o.apps.SetStatus(ctx, appID, here, replica.Ordinal, StatusRunning); err != nil {
			rollback()
			return err
		}
	}
	return nil
}

// Paused stops an app's containers on this machine under its deploy lock,
// runs fn, and starts them again **whether or not fn succeeded** — a backup
// that failed must not also leave the app down. A container that would not
// stop is not paused, and fn does not run.
func (o *Orchestrator) Paused(ctx context.Context, appID int64, fn func() error) error {
	mu := o.lockApp(appID)
	mu.Lock()
	defer mu.Unlock()

	a, err := o.apps.ByID(ctx, appID)
	if err != nil {
		return ErrNotFound
	}
	here, err := o.apps.ControlPlaneID(ctx)
	if err != nil {
		return err
	}
	var stopped []string
	defer func() {
		for _, id := range stopped {
			if err := o.docker.StartContainer(ctx, id); err != nil {
				log.Printf("app %d: could not start container %s again: %v", appID, id, err)
			}
		}
	}()
	for _, mine := range a.ReplicasOn(here) {
		if mine.Container == "" {
			continue
		}
		if err := o.stop(ctx, mine.Container, a); err != nil {
			return fmt.Errorf("stop the app: %w", err)
		}
		stopped = append(stopped, mine.Container)
	}
	return fn()
}

// waitHealthy waits for readiness and preserves the final failure diagnostic.
//
// It requires HealthCheckSuccesses *consecutive* running observations,
// and waits HealthCheckInterval before each one — including the first.
// Checking immediately after ContainerStart proves nothing: Docker
// reports the container running the instant the process is spawned,
// before the app has had any chance to crash. And because every container
// carries RestartPolicy: unless-stopped, a crash-looping app
// intermittently reports running, so a single positive observation can be
// pure luck; requiring a run of them raises the bar without needing
// per-app health configuration.
//
// Configured HTTP/TCP probes must pass on each successful observation.
func (o *Orchestrator) waitHealthy(ctx context.Context, containerID, healthPath string, port int, tcp bool) error {
	needed := o.HealthCheckSuccesses
	if needed < 1 {
		needed = 1
	}

	consecutive := 0
	lastErr := errors.New("container did not become ready")
	for i := 0; i < o.HealthCheckAttempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if o.HealthCheckInterval > 0 {
			// Waiting on ctx as well as the timer: a cancelled deploy
			// must not keep sleeping through every remaining attempt.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(o.HealthCheckInterval):
			}
		}
		running, err := o.docker.IsRunning(ctx, containerID)
		if err != nil || !running {
			if err != nil {
				lastErr = err
			} else {
				lastErr = errors.New("container is not running")
			}
			consecutive = 0
			continue
		}
		if healthPath != "" || tcp {
			probe, ok := o.docker.(healthProber)
			var probeErr error
			if !ok {
				probeErr = errors.New("docker client does not support health probes")
			} else if tcp {
				probeErr = probe.ProbeTCP(ctx, containerID, port)
			} else {
				probeErr = probe.ProbeHTTP(ctx, containerID, healthPath, port)
			}
			if probeErr != nil {
				lastErr = probeErr
				consecutive = 0
				continue
			}
		}
		consecutive++
		if consecutive >= needed {
			return nil
		}
	}
	return lastErr
}
