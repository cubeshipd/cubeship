package dockerx

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeAPI struct {
	pulledRef        string
	createdConfig    *container.Config
	createdName      string
	startedID        string
	stoppedID        string
	removedID        string
	inspectedRunning bool
	inspectErr       error
}

func (f *fakeAPI) ImagePull(ctx context.Context, ref string, options types.ImagePullOptions) (io.ReadCloser, error) {
	f.pulledRef = ref
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *fakeAPI) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	f.createdConfig = config
	f.createdName = containerName
	return container.CreateResponse{ID: "new-container-id"}, nil
}

func (f *fakeAPI) ContainerStart(ctx context.Context, id string, options container.StartOptions) error {
	f.startedID = id
	return nil
}

func (f *fakeAPI) ContainerStop(ctx context.Context, id string, options container.StopOptions) error {
	f.stoppedID = id
	return nil
}

func (f *fakeAPI) ContainerRemove(ctx context.Context, id string, options container.RemoveOptions) error {
	f.removedID = id
	return nil
}

func (f *fakeAPI) ContainerInspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	if f.inspectErr != nil {
		return types.ContainerJSON{}, f.inspectErr
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			State: &types.ContainerState{Running: f.inspectedRunning},
		},
	}, nil
}

func TestCreateContainerForwardsLabelsAndEnv(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	id, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:   "cubeship-myapp-1",
		Image:  "registry.example.com/myapp:latest",
		Labels: map[string]string{"traefik.enable": "true"},
		Env:    []string{"PORT=8080"},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if id != "new-container-id" {
		t.Fatalf("expected new-container-id, got %q", id)
	}
	if fake.createdName != "cubeship-myapp-1" {
		t.Fatalf("expected container name to be forwarded, got %q", fake.createdName)
	}
	if fake.createdConfig.Image != "registry.example.com/myapp:latest" {
		t.Fatalf("expected image to be forwarded, got %q", fake.createdConfig.Image)
	}
	if fake.createdConfig.Labels["traefik.enable"] != "true" {
		t.Fatalf("expected labels to be forwarded, got %v", fake.createdConfig.Labels)
	}
	if len(fake.createdConfig.Env) != 1 || fake.createdConfig.Env[0] != "PORT=8080" {
		t.Fatalf("expected env to be forwarded, got %v", fake.createdConfig.Env)
	}
}

func TestIsRunningTrue(t *testing.T) {
	fake := &fakeAPI{inspectedRunning: true}
	c := newWithAPI(fake)

	running, err := c.IsRunning(context.Background(), "some-id")
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running {
		t.Fatal("expected running=true")
	}
}

func TestIsRunningFalseOnInspectError(t *testing.T) {
	fake := &fakeAPI{inspectErr: errors.New("no such container")}
	c := newWithAPI(fake)

	running, err := c.IsRunning(context.Background(), "gone-id")
	if err == nil {
		t.Fatal("expected an error to be returned")
	}
	if running {
		t.Fatal("expected running=false on error")
	}
}

func TestStopAndRemoveForwardID(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	ctx := context.Background()

	if err := c.StopContainer(ctx, "id-1"); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if fake.stoppedID != "id-1" {
		t.Fatalf("expected stop to forward id-1, got %q", fake.stoppedID)
	}

	if err := c.RemoveContainer(ctx, "id-2"); err != nil {
		t.Fatalf("RemoveContainer: %v", err)
	}
	if fake.removedID != "id-2" {
		t.Fatalf("expected remove to forward id-2, got %q", fake.removedID)
	}
}
