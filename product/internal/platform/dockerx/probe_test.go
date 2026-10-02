package dockerx

import (
	"context"
	"errors"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"strings"
	"testing"
)

func TestProbeUsesRestrictedHelperAndRemovesIt(t *testing.T) {
	f := &fakeAPI{inspectNetworks: map[string]*network.EndpointSettings{ApplicationNetwork: {IPAddress: "192.0.2.10"}}}
	c := newWithAPI(f)
	c.probeInContainer, c.probeImage = true, "sha256:daemon"
	if err := c.ProbeHTTP(context.Background(), "target", "/ready", 8080); err != nil {
		t.Fatal(err)
	}
	h, cfg := f.createdHostConfig, f.createdConfig
	if h == nil || string(h.NetworkMode) != "container:target" {
		t.Fatalf("helper network = %#v", h)
	}
	if cfg.Image != "sha256:daemon" || cfg.User != "65534:65534" || !h.ReadonlyRootfs || h.Privileged || len(h.Binds) != 0 || len(h.PortBindings) != 0 || len(cfg.Env) != 0 {
		t.Fatalf("unsafe helper: %#v %#v", cfg, h)
	}
	if h.RestartPolicy.Name != "no" || len(h.CapDrop) != 1 || h.CapDrop[0] != "ALL" || len(h.SecurityOpt) != 1 || h.SecurityOpt[0] != "no-new-privileges:true" {
		t.Fatalf("unsafe privileges: %#v", h)
	}
	if f.removedID == "" {
		t.Fatal("helper was not removed")
	}
}

func TestProbeWithoutOwnImageFailsClosed(t *testing.T) {
	c := newWithAPI(&fakeAPI{})
	c.probeInContainer = true
	if err := c.ProbeTCP(context.Background(), "target", 8080); err == nil {
		t.Fatal("missing helper image must fail")
	}
}

type canceledProbeAPI struct {
	fakeAPI
	cancel     context.CancelFunc
	cleanupErr error
}

func (f *canceledProbeAPI) ContainerWait(ctx context.Context, id string, condition container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	f.cancel()
	return make(chan container.WaitResponse), make(chan error)
}

func (f *canceledProbeAPI) ContainerRemove(ctx context.Context, id string, opts container.RemoveOptions) error {
	f.cleanupErr = ctx.Err()
	return f.fakeAPI.ContainerRemove(ctx, id, opts)
}

func TestCanceledProbeRemovesHelperWithLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &canceledProbeAPI{fakeAPI: fakeAPI{inspectNetworks: map[string]*network.EndpointSettings{ApplicationNetwork: {IPAddress: "192.0.2.10"}}}, cancel: cancel}
	c := newWithAPI(f)
	c.probeInContainer, c.probeImage = true, "sha256:daemon"
	if err := c.ProbeTCP(ctx, "target", 8080); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if f.removedID == "" || f.cleanupErr != nil {
		t.Fatalf("cleanup id=%q error=%v", f.removedID, f.cleanupErr)
	}
}

func TestProbeRetainsHelperFailureAndCleanupError(t *testing.T) {
	f := &fakeAPI{exitCode: 1, removeErr: errors.New("cleanup failed"), inspectNetworks: map[string]*network.EndpointSettings{ApplicationNetwork: {IPAddress: "192.0.2.10"}}}
	c := newWithAPI(f)
	c.probeInContainer, c.probeImage = true, "sha256:daemon"
	err := c.ProbeHTTP(context.Background(), "target", "/ready", 8080)
	if err == nil || !strings.Contains(err.Error(), "log line 1") || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("lost diagnostic: %v", err)
	}
}

type probeImageAPI struct{ fakeAPI }

func (f *probeImageAPI) ContainerInspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	info, err := f.fakeAPI.ContainerInspect(ctx, id)
	info.Image = "sha256:running-image"
	return info, err
}
func TestConfigureProbesPinsRunningImage(t *testing.T) {
	c := newWithAPI(&probeImageAPI{})
	if err := c.ConfigureProbes(context.Background(), "daemon"); err != nil {
		t.Fatal(err)
	}
	if !c.probeInContainer || c.probeImage != "sha256:running-image" {
		t.Fatalf("probe image = %q", c.probeImage)
	}
}
