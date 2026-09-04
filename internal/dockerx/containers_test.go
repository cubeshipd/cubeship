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
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeAPI struct {
	pulledRef               string
	pulledAuth              string
	pullStream              string
	pullErr                 error
	createdConfig           *container.Config
	createdHostConfig       *container.HostConfig
	createdNetworkingConfig *network.NetworkingConfig
	createdName             string
	startedID               string
	stoppedID               string
	removedID               string
	inspectedID             string
	inspectedName           string
	inspectedRunning        bool
	inspectErr              error
	networkCreateErr        error
}

func (f *fakeAPI) ImagePull(ctx context.Context, ref string, options types.ImagePullOptions) (io.ReadCloser, error) {
	f.pulledRef = ref
	f.pulledAuth = options.RegistryAuth
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return io.NopCloser(strings.NewReader(f.pullStream)), nil
}

func (f *fakeAPI) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	f.createdConfig = config
	f.createdHostConfig = hostConfig
	f.createdNetworkingConfig = networkingConfig
	f.createdName = containerName
	return container.CreateResponse{ID: "new-container-id"}, nil
}

func (f *fakeAPI) NetworkCreate(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
	if f.networkCreateErr != nil {
		return types.NetworkCreateResponse{}, f.networkCreateErr
	}
	return types.NetworkCreateResponse{ID: "net-1"}, nil
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

func (f *fakeAPI) ContainerLogs(ctx context.Context, id string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("log line 1\nlog line 2\n")), nil
}

func (f *fakeAPI) ContainerInspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	f.inspectedName = id
	if f.inspectErr != nil {
		return types.ContainerJSON{}, f.inspectErr
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:    f.inspectedID,
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

func TestLogsReturnsContainerOutput(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	rc, err := c.Logs(context.Background(), "some-id")
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "log line 1\nlog line 2\n" {
		t.Fatalf("unexpected log output: %q", data)
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

func TestCreateContainerForwardsPortsBindsCmdAndNetwork(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	_, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:    "cubeship-traefik",
		Image:   "traefik:v3.1",
		Cmd:     []string{"--api.dashboard=false"},
		Binds:   []string{"/var/run/docker.sock:/var/run/docker.sock:ro"},
		Ports:   []string{"80:80", "443:443"},
		Network: "cubeship",
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if len(fake.createdConfig.Cmd) != 1 || fake.createdConfig.Cmd[0] != "--api.dashboard=false" {
		t.Fatalf("expected Cmd to be forwarded, got %v", fake.createdConfig.Cmd)
	}
	if len(fake.createdConfig.ExposedPorts) != 2 {
		t.Fatalf("expected 2 exposed ports, got %v", fake.createdConfig.ExposedPorts)
	}
	if len(fake.createdHostConfig.Binds) != 1 {
		t.Fatalf("expected 1 bind, got %v", fake.createdHostConfig.Binds)
	}
	if len(fake.createdHostConfig.PortBindings) != 2 {
		t.Fatalf("expected 2 port bindings, got %v", fake.createdHostConfig.PortBindings)
	}
	if fake.createdNetworkingConfig == nil || fake.createdNetworkingConfig.EndpointsConfig["cubeship"] == nil {
		t.Fatalf("expected the container to be attached to the cubeship network")
	}
}

func TestCreateContainerForwardsExtraHosts(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	_, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:       "cubeship-registry",
		Image:      "registry:2",
		Network:    "cubeship",
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if len(fake.createdHostConfig.ExtraHosts) != 1 || fake.createdHostConfig.ExtraHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("expected ExtraHosts to be forwarded, got %v", fake.createdHostConfig.ExtraHosts)
	}
}

func TestCreateContainerHostNetworkSkipsPortsAndNetwork(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	_, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:        "cubeship-traefik",
		Image:       "traefik:v3.1",
		HostNetwork: true,
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if fake.createdHostConfig.NetworkMode != "host" {
		t.Fatalf("expected host network mode, got %q", fake.createdHostConfig.NetworkMode)
	}
	if fake.createdNetworkingConfig != nil {
		t.Fatalf("expected no networking config in host mode, got %v", fake.createdNetworkingConfig)
	}
}

func TestEnsureNetworkSucceedsOnCreate(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if err := c.EnsureNetwork(context.Background(), "cubeship"); err != nil {
		t.Fatalf("EnsureNetwork: %v", err)
	}
}

func TestEnsureNetworkIgnoresAlreadyExists(t *testing.T) {
	for _, existsErr := range []error{
		errors.New(`network with name cubeship already exists`),
		errdefs.Conflict(errors.New("boom")),
	} {
		fake := &fakeAPI{networkCreateErr: existsErr}
		c := newWithAPI(fake)
		if err := c.EnsureNetwork(context.Background(), "cubeship"); err != nil {
			t.Fatalf("expected %v to be treated as already-exists, got %v", existsErr, err)
		}
	}
}

func TestEnsureNetworkReturnsRealErrors(t *testing.T) {
	fake := &fakeAPI{networkCreateErr: errors.New("cannot connect to the docker daemon")}
	c := newWithAPI(fake)

	err := c.EnsureNetwork(context.Background(), "cubeship")
	if err == nil {
		t.Fatal("expected a genuine network creation failure to be returned, not swallowed")
	}
	if !strings.Contains(err.Error(), "cannot connect to the docker daemon") {
		t.Fatalf("expected the underlying error to be wrapped, got %v", err)
	}
}

func TestPullImageReturnsStreamedError(t *testing.T) {
	// The Engine reports most pull failures as HTTP 200 with a JSON
	// error object inside the progress stream.
	fake := &fakeAPI{pullStream: `{"status":"Pulling from myapp"}
{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}
`}
	c := newWithAPI(fake)

	err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:nope")
	if err == nil {
		t.Fatal("expected an in-stream pull error to be returned")
	}
	if !strings.Contains(err.Error(), "manifest unknown") {
		t.Fatalf("expected the streamed message in the error, got %v", err)
	}
}

func TestPullImageReturnsDeprecatedErrorField(t *testing.T) {
	fake := &fakeAPI{pullStream: `{"error":"unauthorized: authentication required"}` + "\n"}
	c := newWithAPI(fake)

	err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:latest")
	if err == nil {
		t.Fatal("expected an error for a stream carrying only the deprecated error field")
	}
	if !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPullImageSucceedsOnCleanStream(t *testing.T) {
	fake := &fakeAPI{pullStream: `{"status":"Pulling from myapp"}
{"status":"Download complete"}
{"status":"Status: Downloaded newer image"}
`}
	c := newWithAPI(fake)

	if err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:latest"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
}

func TestPullImageAttachesRegistryAuthForMatchingHost(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	c.SetRegistryAuth("127.0.0.1:5000", "cubeship", "s3cret")

	if err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:latest"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pulledAuth == "" {
		t.Fatal("expected credentials to be attached to a pull from the authenticated registry")
	}
	decoded, err := registry.DecodeAuthConfig(fake.pulledAuth)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if decoded.Username != "cubeship" || decoded.Password != "s3cret" {
		t.Fatalf("unexpected credentials: %+v", decoded)
	}
}

func TestPullImageSendsNoAuthForOtherHosts(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	c.SetRegistryAuth("127.0.0.1:5000", "cubeship", "s3cret")

	// Bootstrap images come from Docker Hub and must not carry the
	// local registry's credentials.
	if err := c.PullImage(context.Background(), "registry:2"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pulledAuth != "" {
		t.Fatalf("expected no auth for a Docker Hub image, got %q", fake.pulledAuth)
	}

	if err := c.PullImage(context.Background(), "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pulledAuth != "" {
		t.Fatalf("expected no auth for an unregistered host, got %q", fake.pulledAuth)
	}
}

func TestInspectContainerByNameReturnsIDAndRunning(t *testing.T) {
	fake := &fakeAPI{inspectedID: "abc123", inspectedRunning: true}
	c := newWithAPI(fake)

	id, running, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if err != nil {
		t.Fatalf("InspectContainerByName: %v", err)
	}
	if fake.inspectedName != "cubeship-traefik" {
		t.Fatalf("expected the name to be forwarded, got %q", fake.inspectedName)
	}
	if id != "abc123" || !running {
		t.Fatalf("unexpected result: id=%q running=%v", id, running)
	}
}

func TestInspectContainerByNameNotFound(t *testing.T) {
	fake := &fakeAPI{inspectErr: errdefs.NotFound(errors.New("no such container: cubeship-traefik"))}
	c := newWithAPI(fake)

	_, _, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("expected ErrContainerNotFound, got %v", err)
	}
}

func TestInspectContainerByNameReturnsOtherErrors(t *testing.T) {
	fake := &fakeAPI{inspectErr: errors.New("cannot connect to the docker daemon")}
	c := newWithAPI(fake)

	_, _, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if err == nil || errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("expected a real error to be returned, got %v", err)
	}
}
