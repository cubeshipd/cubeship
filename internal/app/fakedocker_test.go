package app

import (
	"context"
	"io"
	"strings"
	"sync"

	"cubeship/internal/platform/dockerx"
)

// fakeDocker stands in for the Docker Engine. It records what it was
// asked to do, in order, so a test can assert on the sequence a deploy
// performs rather than only its outcome.
type fakeDocker struct {
	mu sync.Mutex

	nextCreateID string
	running      bool
	// runningSeq, when non-empty, is consumed one entry per IsRunning
	// call (falling back to `running` once exhausted), so a test can
	// script a flapping container.
	runningSeq []bool

	pulledRefs  []string
	createdOpts []dockerx.ContainerOpts
	startedIDs  []string
	stoppedIDs  []string
	removedIDs  []string
	logOutput   string

	pullErr   error
	createErr error
	startErr  error
	removeErr error
}

func (f *fakeDocker) PullImage(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pullErr != nil {
		return f.pullErr
	}
	f.pulledRefs = append(f.pulledRefs, ref)
	return nil
}

func (f *fakeDocker) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createdOpts = append(f.createdOpts, opts)
	return f.nextCreateID, nil
}

func (f *fakeDocker) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.startedIDs = append(f.startedIDs, id)
	return nil
}

func (f *fakeDocker) StopContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stoppedIDs = append(f.stoppedIDs, id)
	return nil
}

func (f *fakeDocker) RemoveContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *fakeDocker) IsRunning(_ context.Context, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runningSeq) > 0 {
		next := f.runningSeq[0]
		f.runningSeq = f.runningSeq[1:]
		return next, nil
	}
	return f.running, nil
}

func (f *fakeDocker) Logs(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logOutput)), nil
}

func (f *fakeDocker) snapshot() (created []dockerx.ContainerOpts, started, stopped, removed []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dockerx.ContainerOpts(nil), f.createdOpts...),
		append([]string(nil), f.startedIDs...),
		append([]string(nil), f.stoppedIDs...),
		append([]string(nil), f.removedIDs...)
}
