package dockerx

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeAPI struct {
	pulledRef  string
	pulledAuth string
	pullStream string
	// pullStreams, when set, is drained one entry per ImagePull call so
	// a retry can see a transient failure and then a clean stream.
	pullStreams             []string
	pullCalls               int
	pullErr                 error
	createdConfig           *container.Config
	createdHostConfig       *container.HostConfig
	createdNetworkingConfig *network.NetworkingConfig
	createdName             string
	startedID               string
	stoppedID               string
	removedID               string
	removeErr               error
	updatedID               string
	updatedResources        container.Resources
	inspectedID             string
	inspectedName           string
	inspectedRunning        bool
	startedAt               string
	inspectErr              error
	inspectNetworks         map[string]*network.EndpointSettings
	networkCreateErr        error
	loggedID                string
	loggedOptions           container.LogsOptions
	localImages             map[string]bool
	containers              []types.Container
	exitCode                int
	waitedBeforeStart       bool
	statsID                 string
	statsJSON               string
	loaded                  []byte
	loadStream              string
	swarm                   swarm.Info
	networks                map[string]bool
	networkConnectErr       error
	connected               []string
	// imageUser is the USER every local image says it runs as, and
	// archives what CopyFromContainer answers, by path.
	imageUser string
	archives  map[string][]byte
}

func (f *fakeAPI) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
	f.pulledRef = ref
	f.pulledAuth = options.RegistryAuth
	f.pullCalls++
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	stream := f.pullStream
	if len(f.pullStreams) > 0 {
		stream = f.pullStreams[0]
		f.pullStreams = f.pullStreams[1:]
	}
	return io.NopCloser(strings.NewReader(stream)), nil
}

func (f *fakeAPI) ImageLoad(_ context.Context, r io.Reader, _ ...client.ImageLoadOption) (image.LoadResponse, error) {
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

// The Engine's clustering, which this fake answers for and no test here
// exercises: the overlay network is the one thing Cubeship asks swarm
// mode for, and proving it works needs two machines rather than a fake.
func (f *fakeAPI) Info(context.Context) (system.Info, error) {
	return system.Info{Swarm: f.swarm}, nil
}

func (f *fakeAPI) SwarmInit(context.Context, swarm.InitRequest) (string, error) {
	f.swarm = swarm.Info{LocalNodeState: swarm.LocalNodeStateActive, ControlAvailable: true, NodeID: "node-1"}
	return f.swarm.NodeID, nil
}

func (f *fakeAPI) SwarmInspect(context.Context) (swarm.Swarm, error) {
	return swarm.Swarm{JoinTokens: swarm.JoinTokens{Worker: "a-worker-token"}}, nil
}

func (f *fakeAPI) SwarmJoin(context.Context, swarm.JoinRequest) error {
	f.swarm = swarm.Info{LocalNodeState: swarm.LocalNodeStateActive, NodeID: "node-2"}
	return nil
}

func (f *fakeAPI) NetworkConnect(_ context.Context, networkID, containerID string, _ *network.EndpointSettings) error {
	if f.networkConnectErr != nil {
		return f.networkConnectErr
	}
	f.connected = append(f.connected, networkID+":"+containerID)
	return nil
}

func (f *fakeAPI) NetworkInspect(_ context.Context, name string, _ network.InspectOptions) (network.Inspect, error) {
	if f.networks[name] {
		return network.Inspect{Name: name}, nil
	}
	return network.Inspect{}, errdefs.NotFound(errors.New("no such network"))
}

func (f *fakeAPI) NetworkDisconnect(context.Context, string, string, bool) error { return nil }

func (f *fakeAPI) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	if f.networkCreateErr != nil {
		return network.CreateResponse{}, f.networkCreateErr
	}
	return network.CreateResponse{ID: "net-1"}, nil
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
	return f.removeErr
}

func (f *fakeAPI) ContainerUpdate(_ context.Context, id string, cfg container.UpdateConfig) (container.ContainerUpdateOKBody, error) {
	f.updatedID = id
	f.updatedResources = cfg.Resources
	return container.ContainerUpdateOKBody{}, nil
}

func (f *fakeAPI) ImageInspectWithRaw(_ context.Context, ref string) (types.ImageInspect, []byte, error) {
	if f.localImages[ref] {
		info := types.ImageInspect{ID: "sha256:" + ref}
		if f.imageUser != "" {
			info.Config = &dockerspec.DockerOCIImageConfig{}
			info.Config.User = f.imageUser
		}
		return info, nil, nil
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

// statsJSON is what the Engine answers a one-shot stats request with.
// Empty by default: the fake exists for the tests below, and the ones
// about stats set it.
// containers is what ContainerList answers with, for the tests about
// which ports are published.
func (f *fakeAPI) ContainerList(context.Context, container.ListOptions) ([]types.Container, error) {
	return f.containers, nil
}

func (f *fakeAPI) ContainerWait(_ context.Context, _ string, _ container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	if f.startedID == "" {
		f.waitedBeforeStart = true
	}
	done := make(chan container.WaitResponse, 1)
	done <- container.WaitResponse{StatusCode: int64(f.exitCode)}
	return done, make(chan error, 1)
}

func (f *fakeAPI) ContainerStatsOneShot(ctx context.Context, id string) (container.StatsResponseReader, error) {
	f.statsID = id
	body := f.statsJSON
	if body == "" {
		body = "{}"
	}
	return container.StatsResponseReader{Body: io.NopCloser(strings.NewReader(body))}, nil
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
			State: &types.ContainerState{Running: f.inspectedRunning, StartedAt: f.startedAt},
		},
		NetworkSettings: &types.NetworkSettings{Networks: f.inspectNetworks},
	}, nil
}

func TestContainerStartedAtReadsCurrentDockerStart(t *testing.T) {
	want := time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC)
	fake := &fakeAPI{inspectedRunning: true, startedAt: want.Format(time.RFC3339Nano)}
	got, err := newWithAPI(fake).ContainerStartedAt(context.Background(), "container-id")
	if err != nil {
		t.Fatalf("ContainerStartedAt: %v", err)
	}
	if got == nil || !got.Equal(want) {
		t.Fatalf("ContainerStartedAt = %v, want %v", got, want)
	}
}

func TestProbeTCPConnectsAndReportsConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	fake := &fakeAPI{inspectNetworks: map[string]*network.EndpointSettings{
		ApplicationNetwork: {IPAddress: "127.0.0.1"},
	}}
	c := newWithAPI(fake)
	if err := c.ProbeTCP(context.Background(), "id", port); err != nil {
		t.Fatalf("ProbeTCP: %v", err)
	}
	listener.Close()
	if err := c.ProbeTCP(context.Background(), "id", port); err == nil {
		t.Fatal("ProbeTCP succeeded after listener closed")
	}
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
