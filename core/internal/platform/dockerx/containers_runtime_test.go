package dockerx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/errdefs"
)

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

// The percentage the dashboard charts is computed from these counters,
// so what matters is that they come back unmangled — and that memory is
// what `docker stats` shows rather than what the cgroup counts, which
// includes page cache the kernel would hand straight back.
func TestContainerStatsSubtractsReclaimablePageCache(t *testing.T) {
	fake := &fakeAPI{statsJSON: `{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 900},
			"system_cpu_usage": 9000,
			"online_cpus": 4
		},
		"memory_stats": {
			"usage": 500,
			"limit": 2000,
			"stats": {"inactive_file": 200}
		}
	}`}
	c := newWithAPI(fake)

	stats, err := c.ContainerStats(context.Background(), "some-id")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if fake.statsID != "some-id" {
		t.Errorf("asked about %q", fake.statsID)
	}
	if stats.CPUTotal != 900 || stats.CPUSystem != 9000 {
		t.Errorf("CPU counters came back as %d/%d", stats.CPUTotal, stats.CPUSystem)
	}
	if stats.OnlineCPUs != 4 {
		t.Errorf("online CPUs is %d; 100%% means one core, so this is what makes 400%% possible", stats.OnlineCPUs)
	}
	if stats.MemoryBytes != 300 {
		t.Errorf("memory is %d, want 500 minus the 200 of reclaimable cache", stats.MemoryBytes)
	}
	if stats.MemoryLimit != 2000 {
		t.Errorf("limit is %d", stats.MemoryLimit)
	}
}

// cgroup v1 spells the same number differently, and a host on it must
// not read as though every container were about to run out of memory.
func TestContainerStatsHandlesCgroupV1(t *testing.T) {
	fake := &fakeAPI{statsJSON: `{
		"cpu_stats": {"cpu_usage": {"percpu_usage": [1, 2]}},
		"memory_stats": {"usage": 500, "stats": {"total_inactive_file": 100}}
	}`}
	c := newWithAPI(fake)

	stats, err := c.ContainerStats(context.Background(), "some-id")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if stats.MemoryBytes != 400 {
		t.Errorf("memory is %d, want 500 minus 100", stats.MemoryBytes)
	}
	// An Engine that reports no online_cpus still says how many there
	// are, in the per-CPU slice.
	if stats.OnlineCPUs != 2 {
		t.Errorf("online CPUs is %d, want the length of percpu_usage", stats.OnlineCPUs)
	}
}

// A host that answers neither must not make the percentage a division
// by zero, or worse, always zero.
func TestContainerStatsNeverReportsZeroCPUs(t *testing.T) {
	fake := &fakeAPI{statsJSON: `{"memory_stats": {"usage": 10}}`}
	c := newWithAPI(fake)

	stats, err := c.ContainerStats(context.Background(), "some-id")
	if err != nil {
		t.Fatalf("ContainerStats: %v", err)
	}
	if stats.OnlineCPUs != 1 {
		t.Errorf("online CPUs is %d, want 1", stats.OnlineCPUs)
	}
}

// What is exposed on a Docker host is what containers publish, not what
// the host's own services listen on — and a port bound to loopback is
// exposed to nothing at all.
func TestOnlyPortsSomethingOutsideCouldReachAreReported(t *testing.T) {
	fake := &fakeAPI{containers: []types.Container{
		{
			Names: []string{"/cubeship-traefik"},
			Ports: []types.Port{
				{PublicPort: 443, PrivatePort: 443, Type: "tcp", IP: "0.0.0.0"},
				{PublicPort: 443, PrivatePort: 443, Type: "tcp", IP: "::"},
			},
		},
		{
			// Postgres publishes on loopback, which is how the daemon
			// reaches it and why nothing outside can. Offering to open
			// it would be offering a hole for something never exposed.
			Names: []string{"/cubeship-postgres"},
			Ports: []types.Port{{PublicPort: 5433, PrivatePort: 5432, Type: "tcp", IP: "127.0.0.1"}},
		},
		{
			// A container that publishes nothing at all.
			Names: []string{"/cubeship-frontend"},
			Ports: []types.Port{{PrivatePort: 3000, Type: "tcp"}},
		},
	}}

	got, err := newWithAPI(fake).PublishedPorts(context.Background())
	if err != nil {
		t.Fatalf("PublishedPorts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("reported %+v", got)
	}
	// One port, though it is published on two address families: a rule
	// is about the port.
	if got[0].Port != 443 || got[0].Container != "cubeship-traefik" {
		t.Errorf("reported %+v", got[0])
	}
}

// A one-shot has to be started before it is waited on.
//
// It was the other way round, on the theory that a fast command could
// exit before the wait was registered. That race does not exist. The one
// that does is the mirror image: a container that has been created and
// not started is already "not running", so the wait returns at once —
// exit code 0, empty log — before the command has done anything, and
// the caller cannot tell that from success.
//
// The symptom is the worst kind: every command reports success and has
// no effect. Reads come back empty and writes never land.
func TestAOneShotIsStartedBeforeItIsWaitedOn(t *testing.T) {
	fake := &fakeAPI{}
	if _, _, err := newWithAPI(fake).RunOneShot(context.Background(), ContainerOpts{
		Image: "alpine", Cmd: []string{"true"},
	}); err != nil {
		t.Fatalf("RunOneShot: %v", err)
	}
	if fake.waitedBeforeStart {
		t.Error("it waited on a container it had not started, which always returns immediately")
	}
	if fake.startedID == "" {
		t.Error("nothing was started")
	}
	if fake.removedID == "" {
		t.Error("one-shot container was not cleaned up")
	}
}

// A remove asks for the container to be gone. One somebody already
// removed by hand is gone, so the request is satisfied — and saying no
// instead made "stop this app" fail permanently for an app whose
// container had been removed underneath the daemon, which is the one
// state where being refused helps least.
func TestRemovingAContainerThatIsAlreadyGoneSucceeds(t *testing.T) {
	c := newWithAPI(&fakeAPI{removeErr: errdefs.NotFound(errors.New("no such container: c1"))})
	if err := c.RemoveContainer(context.Background(), "c1"); err != nil {
		t.Errorf("removing a container that is not there: %v", err)
	}

	// Anything else is still an error: a daemon that refused for its own
	// reasons has not removed anything.
	c = newWithAPI(&fakeAPI{removeErr: errors.New("device or resource busy")})
	if err := c.RemoveContainer(context.Background(), "c1"); err == nil {
		t.Error("a refused remove reported success")
	}
}

// A container that has to be reachable from another machine is on the
// local bridge *and* on the cluster's overlay. The bridge stays because
// everything on this box already resolves it there; the overlay is what
// the other machines resolve it on.
//
// Connected after create and **before start**, which is the part worth
// pinning: a server that binds or registers at startup would otherwise
// come up without the interface.
func TestAContainerJoinsTheClusterNetworkBeforeItStarts(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	id, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:         "cubeship-db-pg",
		Image:        "postgres:17",
		Network:      "cubeship",
		AlsoNetworks: []string{"cubeship-mesh"},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if len(fake.connected) != 1 || fake.connected[0] != "cubeship-mesh:"+id {
		t.Fatalf("connected %v, want the overlay", fake.connected)
	}
	if fake.startedID != "" {
		t.Error("the container was started before it was on every network it was asked for")
	}
	// The bridge is passed at create and not connected again.
	if _, ok := fake.createdNetworkingConfig.EndpointsConfig["cubeship"]; !ok {
		t.Error("the local bridge is not on the container")
	}
}

// A container on fewer networks than it was asked for is not the
// container that was asked for: it would come up, pass its health check
// and be unreachable from half the cluster. So the create fails, and
// what it half-made is removed rather than left running.
func TestAContainerThatCannotJoinTheClusterIsNotLeftRunning(t *testing.T) {
	fake := &fakeAPI{networkConnectErr: errors.New("no such network")}
	c := newWithAPI(fake)

	if _, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name: "cubeship-db-pg", Image: "postgres:17",
		Network: "cubeship", AlsoNetworks: []string{"cubeship-mesh"},
	}); err == nil {
		t.Fatal("a container that could not join the cluster came back as created")
	}
	if fake.removedID != "new-container-id" {
		t.Errorf("removed %q, want the container that was half-made", fake.removedID)
	}
	if fake.startedID != "" {
		t.Error("it was started anyway")
	}
}

// An instance of one machine has no cluster network, and asking for one
// would be a container that cannot be created at all.
func TestWithNoClusterNetworkNothingExtraIsAskedFor(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if _, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name: "cubeship-db-pg", Image: "postgres:17", Network: "cubeship",
	}); err != nil {
		t.Fatal(err)
	}
	if len(fake.connected) != 0 {
		t.Errorf("connected %v on an instance with no cluster", fake.connected)
	}
}

// The network the daemon decides by: it is there or it is not, and a
// Docker that says "no such network" is an instance of one machine
// rather than a failure to report.
func TestAMissingNetworkIsAnAnswerRatherThanAnError(t *testing.T) {
	fake := &fakeAPI{networks: map[string]bool{"cubeship-mesh": true}}
	c := newWithAPI(fake)

	found, err := c.NetworkExists(context.Background(), "cubeship-mesh")
	if err != nil || !found {
		t.Errorf("NetworkExists(existing) = %v, %v", found, err)
	}
	found, err = c.NetworkExists(context.Background(), "cubeship-mesh-2")
	if err != nil {
		t.Errorf("a network that is not there was an error: %v", err)
	}
	if found {
		t.Error("a network that is not there was found")
	}
}

func TestACeilingReachesTheEngineOnCreate(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if _, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name:      "cubeship-myapp-1",
		Image:     "myapp:latest",
		Resources: Resources{NanoCPUs: 1_500_000_000, MemoryBytes: 512 << 20},
	}); err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	got := fake.createdHostConfig.Resources
	if got.NanoCPUs != 1_500_000_000 {
		t.Errorf("the quota reached the Engine as %d", got.NanoCPUs)
	}
	if got.Memory != 512<<20 {
		t.Errorf("the memory ceiling reached the Engine as %d", got.Memory)
	}
	// Left alone, Docker allows twice the memory limit in swap, so the
	// number would mean one thing on a host with swap and another on a
	// host without.
	if got.MemorySwap != 512<<20 {
		t.Errorf("swap is %d, want it pinned to the ceiling", got.MemorySwap)
	}
}

func TestNoCeilingIsNoSwapSetting(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if _, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name: "cubeship-myapp-1", Image: "myapp:latest",
	}); err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if got := fake.createdHostConfig.Resources; got.Memory != 0 || got.MemorySwap != 0 || got.NanoCPUs != 0 {
		t.Errorf("an uncapped container was given %+v", got)
	}
}

func TestSetResourcesUpdatesTheContainerInPlace(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if err := c.SetResources(context.Background(), "abc123", Resources{NanoCPUs: 500_000_000, MemoryBytes: 256 << 20}); err != nil {
		t.Fatalf("SetResources: %v", err)
	}
	if fake.updatedID != "abc123" {
		t.Errorf("it updated %q", fake.updatedID)
	}
	if fake.updatedResources.NanoCPUs != 500_000_000 || fake.updatedResources.Memory != 256<<20 {
		t.Errorf("it sent %+v", fake.updatedResources)
	}
}

// **AutoRemove has to reach the Engine.** It was declared and never
// passed, which nothing noticed: the one caller that used it removes
// its container itself. The one that did not was the updater, and a
// leftover updater is a name already in use — so the second update an
// instance ever ran failed on it, after the dashboard was replaced and
// before the daemon was.
func TestAOneShotIsAskedToRemoveItself(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if _, err := c.CreateContainer(context.Background(), ContainerOpts{
		Name: "cubeship-daemon-updater", Image: "cubeshipd:0.3.1", AutoRemove: true,
	}); err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	if !fake.createdHostConfig.AutoRemove {
		t.Error("the Engine was not told to remove it, so its name is taken until somebody does")
	}
	// And Docker refuses a restart policy alongside it.
	if fake.createdHostConfig.RestartPolicy.Name != "" {
		t.Errorf("it also carries a restart policy: %q", fake.createdHostConfig.RestartPolicy.Name)
	}
}

func (f *fakeAPI) ContainerExecResize(context.Context, string, container.ResizeOptions) error {
	return nil
}

func (f *fakeAPI) ContainerResize(context.Context, string, container.ResizeOptions) error {
	return nil
}

func (f *fakeAPI) ContainerAttach(context.Context, string, container.AttachOptions) (types.HijackedResponse, error) {
	return types.HijackedResponse{}, errors.New("not faked")
}

func (f *fakeAPI) ContainerStatPath(context.Context, string, string) (container.PathStat, error) {
	return container.PathStat{}, nil
}

func TestProbeHTTPDoesNotFollowRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	host, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(rawPort)
	c := newWithAPI(&fakeAPI{inspectNetworks: map[string]*network.EndpointSettings{ApplicationNetwork: {IPAddress: host}}})
	if err := c.ProbeHTTP(context.Background(), "app", "/health", port); err == nil {
		t.Fatal("redirect must not count as a healthy response")
	}
}
