package dockerx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
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
	loggedID                string
	loggedOptions           container.LogsOptions
	localImages             map[string]bool
	loaded                  []byte
	loadStream              string
}

func (f *fakeAPI) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	f.pulledRef = ref
	f.pulledAuth = options.RegistryAuth
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return io.NopCloser(strings.NewReader(f.pullStream)), nil
}

func (f *fakeAPI) ImageLoad(_ context.Context, r io.Reader, _ bool) (image.LoadResponse, error) {
	loaded, err := io.ReadAll(r)
	f.loaded = loaded
	if err != nil {
		return image.LoadResponse{}, err
	}
	return image.LoadResponse{
		Body: io.NopCloser(strings.NewReader(f.loadStream)),
		JSON: f.loadStream != "",
	}, nil
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

func (f *fakeAPI) ImageInspectWithRaw(_ context.Context, ref string) (types.ImageInspect, []byte, error) {
	if f.localImages[ref] {
		return types.ImageInspect{ID: "sha256:" + ref}, nil, nil
	}
	return types.ImageInspect{}, nil, errdefs.NotFound(errors.New("no such image"))
}

// The exec trio is here so the fake satisfies the interface. Nothing in
// this package's tests runs a command in a container — what does is a
// registry garbage collection, which needs a real registry to have
// written blobs for it to walk.
func (f *fakeAPI) ContainerExecCreate(context.Context, string, container.ExecOptions) (types.IDResponse, error) {
	return types.IDResponse{ID: "exec-1"}, nil
}

func (f *fakeAPI) ContainerExecAttach(context.Context, string, container.ExecAttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, errors.New("this fake does not run commands")
}

func (f *fakeAPI) ContainerExecInspect(context.Context, string) (container.ExecInspect, error) {
	return container.ExecInspect{}, nil
}

func (f *fakeAPI) ContainerLogs(ctx context.Context, id string, options container.LogsOptions) (io.ReadCloser, error) {
	f.loggedID = id
	f.loggedOptions = options
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

	rc, err := c.Logs(context.Background(), "some-id", "")
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "log line 1\nlog line 2\n" {
		t.Fatalf("unexpected log output: %q", data)
	}
	if fake.loggedOptions.Tail != "" {
		t.Fatalf("expected an empty tail to request the full log, got %q", fake.loggedOptions.Tail)
	}
}

func TestLogsForwardsTail(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if _, err := c.Logs(context.Background(), "some-id", "200"); err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if fake.loggedID != "some-id" {
		t.Fatalf("expected container %q, got %q", "some-id", fake.loggedID)
	}
	if fake.loggedOptions.Tail != "200" {
		t.Fatalf("expected tail 200 to be forwarded, got %q", fake.loggedOptions.Tail)
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

	err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:nope", nil)
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

	err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:latest", nil)
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

	if err := c.PullImage(context.Background(), "127.0.0.1:5000/myapp:latest", nil); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
}

func TestPullImageAttachesRegistryAuthForMatchingHost(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	var signedFor string
	c.SetRegistryTokenSigner("127.0.0.1:5000", func(repository string) (string, error) {
		signedFor = repository
		return "signed-jwt", nil
	})

	if err := c.PullImage(context.Background(), "127.0.0.1:5000/acme/myapp:latest", nil); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if signedFor != "acme/myapp" {
		t.Fatalf("expected the signer to be called with repository acme/myapp, got %q", signedFor)
	}
	if fake.pulledAuth == "" {
		t.Fatal("expected credentials to be attached to a pull from the authenticated registry")
	}
	decoded, err := registry.DecodeAuthConfig(fake.pulledAuth)
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}
	if decoded.RegistryToken != "signed-jwt" {
		t.Fatalf("unexpected credentials: %+v", decoded)
	}
}

func TestPullImagePropagatesSignerError(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	c.SetRegistryTokenSigner("127.0.0.1:5000", func(repository string) (string, error) {
		return "", fmt.Errorf("signing key unavailable")
	})

	err := c.PullImage(context.Background(), "127.0.0.1:5000/acme/myapp:latest", nil)
	if err == nil {
		t.Fatal("expected an error when the signer fails")
	}
}

func TestPullImageSendsNoAuthForOtherHosts(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)
	c.SetRegistryTokenSigner("127.0.0.1:5000", func(repository string) (string, error) {
		return "signed-jwt", nil
	})

	// Bootstrap images come from Docker Hub and must not carry the
	// local registry's credentials.
	if err := c.PullImage(context.Background(), "registry:2", nil); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pulledAuth != "" {
		t.Fatalf("expected no auth for a Docker Hub image, got %q", fake.pulledAuth)
	}

	if err := c.PullImage(context.Background(), "registry.example.com/myapp:latest", nil); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pulledAuth != "" {
		t.Fatalf("expected no auth for an unregistered host, got %q", fake.pulledAuth)
	}
}

func TestInspectContainerByNameReturnsIDAndRunning(t *testing.T) {
	fake := &fakeAPI{inspectedID: "abc123", inspectedRunning: true}
	c := newWithAPI(fake)

	info, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if err != nil {
		t.Fatalf("InspectContainerByName: %v", err)
	}
	if fake.inspectedName != "cubeship-traefik" {
		t.Fatalf("expected the name to be forwarded, got %q", fake.inspectedName)
	}
	if info.ID != "abc123" || !info.Running {
		t.Fatalf("unexpected result: id=%q running=%v", info.ID, info.Running)
	}
}

func TestInspectContainerByNameNotFound(t *testing.T) {
	fake := &fakeAPI{inspectErr: errdefs.NotFound(errors.New("no such container: cubeship-traefik"))}
	c := newWithAPI(fake)

	_, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("expected ErrContainerNotFound, got %v", err)
	}
}

func TestInspectContainerByNameReturnsOtherErrors(t *testing.T) {
	fake := &fakeAPI{inspectErr: errors.New("cannot connect to the docker daemon")}
	c := newWithAPI(fake)

	_, err := c.InspectContainerByName(context.Background(), "cubeship-traefik")
	if err == nil || errors.Is(err, ErrContainerNotFound) {
		t.Fatalf("expected a real error to be returned, got %v", err)
	}
}
