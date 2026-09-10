package app_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/server/servertest"
)

// cappingDocker is a Docker that says yes and remembers what ceilings it
// was given, both at create time and afterwards.
type cappingDocker struct {
	mu      sync.Mutex
	created []dockerx.ContainerOpts
	capped  []dockerx.Resources
}

func (d *cappingDocker) CreateContainer(_ context.Context, opts dockerx.ContainerOpts) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.created = append(d.created, opts)
	return "container-abc", nil
}

func (d *cappingDocker) SetResources(_ context.Context, _ string, r dockerx.Resources) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.capped = append(d.capped, r)
	return nil
}

func (d *cappingDocker) ceilings() []dockerx.Resources {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dockerx.Resources(nil), d.capped...)
}

func (d *cappingDocker) createdWith() []dockerx.ContainerOpts {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]dockerx.ContainerOpts(nil), d.created...)
}

func (d *cappingDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return nil }
func (d *cappingDocker) StartContainer(context.Context, string) error                   { return nil }
func (d *cappingDocker) StopContainer(context.Context, string) error                    { return nil }
func (d *cappingDocker) RemoveContainer(context.Context, string) error                  { return nil }
func (d *cappingDocker) IsRunning(context.Context, string) (bool, error)                { return true, nil }
func (d *cappingDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

// **Raising a limit is a request, not a redeploy.** A ceiling is the one
// part of a container the Engine can change under a running process, so
// an app that needs more memory gets it without its container being
// replaced and without a pull.
func TestANewCeilingReachesTheRunningContainer(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	deploy(t, f, created.Reference)

	before := len(docker.createdWith())
	place(t, f, created.Reference, map[string]any{
		"limits": map[string]any{"cpu": 1.5, "memory_bytes": 512 << 20},
	})

	ceilings := docker.ceilings()
	if len(ceilings) != 1 {
		t.Fatalf("the Engine was asked to set %d ceilings", len(ceilings))
	}
	want := dockerx.Resources{NanoCPUs: 1_500_000_000, MemoryBytes: 512 << 20}
	if ceilings[0] != want {
		t.Errorf("it was asked for %+v", ceilings[0])
	}
	if now := len(docker.createdWith()); now != before {
		t.Errorf("%d containers were created for a limit change", now-before)
	}
}

// The stored ceiling is what the next container is created under, so an
// app that is redeployed does not quietly come back uncapped.
func TestTheNextContainerIsCreatedUnderTheCeiling(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{
		"limits": map[string]any{"cpu": 0.5, "memory_bytes": 256 << 20},
	})
	deploy(t, f, created.Reference)

	opts := docker.createdWith()
	if len(opts) == 0 {
		t.Fatal("nothing was created")
	}
	want := dockerx.Resources{NanoCPUs: 500_000_000, MemoryBytes: 256 << 20}
	if got := opts[len(opts)-1].Resources; got != want {
		t.Errorf("the container was created with %+v", got)
	}
}

// A machine has no database to ask what an app may use, so the ceiling
// is part of the instruction it is sent.
func TestAMachineIsToldTheCeilingToRunUnder(t *testing.T) {
	f := balancerFixture(t)
	token := addServer(t, f, "eu-1")
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{
		"nodes":  []string{"eu-1"},
		"limits": map[string]any{"cpu": 2, "memory_bytes": 1 << 30},
	})
	deploy(t, f, created.Reference)

	answer := reconcile(t, f, token)
	if len(answer.Desired.Apps) != 1 {
		t.Fatalf("the machine was told to run %d apps", len(answer.Desired.Apps))
	}
	want := dockerx.Resources{NanoCPUs: 2_000_000_000, MemoryBytes: 1 << 30}
	if got := answer.Desired.Apps[0].Resources; got != want {
		t.Errorf("it was told to run it under %+v", got)
	}
}

// A number the Engine would refuse is refused here, where the person who
// typed it is still watching — rather than on whatever machine tried it,
// minutes later, as a container that would not start.
func TestACeilingTooSmallToMeanAnythingIsRefused(t *testing.T) {
	f := servertest.New(t)
	created := createExternalApp(t, f, "api")

	for _, body := range []map[string]any{
		{"cpu": 0.001},
		{"memory_bytes": 1 << 20},
		{"cpu": -1},
	} {
		rec := f.Do(t, http.MethodPatch, "/apps/"+created.Reference,
			map[string]any{"limits": body}, f.AdminKey)
		servertest.RequireStatus(t, rec, http.StatusBadRequest)
	}
}

// Zero is a value here, not a gap: it is the only way to say "no limit",
// so it has to reach the column rather than be read as "leave it alone".
func TestZeroClearsTheCeiling(t *testing.T) {
	docker := &cappingDocker{}
	f := servertest.NewWithDocker(t, docker)
	created := createExternalApp(t, f, "api")
	place(t, f, created.Reference, map[string]any{
		"limits": map[string]any{"cpu": 1, "memory_bytes": 512 << 20},
	})

	var out struct {
		Limits struct {
			CPU    float64 `json:"cpu"`
			Memory int64   `json:"memory_bytes"`
		} `json:"limits"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPatch, "/apps/"+created.Reference,
		map[string]any{"limits": map[string]any{"cpu": 0, "memory_bytes": 0}},
		f.AdminKey, &out), http.StatusOK)
	if out.Limits.CPU != 0 || out.Limits.Memory != 0 {
		t.Errorf("the app still reports %+v", out.Limits)
	}
	// And it takes a new container to actually lift it, which is what
	// the next deploy is: the Engine reads a zero in an update as
	// "leave that one alone".
	deploy(t, f, created.Reference)
	opts := docker.createdWith()
	if got := opts[len(opts)-1].Resources; !got.Unlimited() {
		t.Errorf("the replacement came up under %+v", got)
	}
}
