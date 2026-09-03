package bootstrap

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"cubeship/internal/config"
	"cubeship/internal/dockerx"
)

func testConfig() *config.Config {
	return &config.Config{
		Domain:       "example.com",
		RegistryHost: "registry.example.com",
		APIHost:      "api.example.com",
		DataDir:      "/var/lib/cubeship",
	}
}

func TestRegistryContainerOptsRoutesThroughTraefik(t *testing.T) {
	opts := RegistryContainerOpts(testConfig(), "http://127.0.0.1:9000/hooks/registry")

	if opts.Name != "cubeship-registry" {
		t.Fatalf("unexpected name: %q", opts.Name)
	}
	if opts.Labels["traefik.http.routers.cubeship-registry.rule"] != "Host(`registry.example.com`)" {
		t.Fatalf("expected registry to be routed via Traefik, got %v", opts.Labels)
	}
	if opts.Network != "cubeship" {
		t.Fatalf("expected the registry on the cubeship network, got %q", opts.Network)
	}
	if len(opts.Ports) != 1 || opts.Ports[0] != "127.0.0.1:5000:5000" {
		t.Fatalf("expected the registry published on localhost:5000, got %v", opts.Ports)
	}

	found := false
	for _, e := range opts.Env {
		if e == "REGISTRY_NOTIFICATIONS_ENDPOINTS_0_URL=http://127.0.0.1:9000/hooks/registry" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the notify URL in env, got %v", opts.Env)
	}
}

func TestTraefikContainerOptsUsesHostNetwork(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), "admin@example.com")

	if !opts.HostNetwork {
		t.Fatal("expected Traefik to run with host networking")
	}
	if len(opts.Binds) != 3 {
		t.Fatalf("expected docker socket + acme storage + dynamic config binds, got %v", opts.Binds)
	}
	hasEmailFlag := false
	for _, c := range opts.Cmd {
		if c == "--certificatesresolvers.letsencrypt.acme.email=admin@example.com" {
			hasEmailFlag = true
		}
	}
	if !hasEmailFlag {
		t.Fatalf("expected the ACME email flag, got %v", opts.Cmd)
	}
	hasFileProvider := false
	for _, c := range opts.Cmd {
		if c == "--providers.file.directory=/etc/traefik/dynamic" {
			hasFileProvider = true
		}
	}
	if !hasFileProvider {
		t.Fatalf("expected the file provider flag, got %v", opts.Cmd)
	}
}

func TestAPIRouterConfigYAMLRoutesToDaemonPort(t *testing.T) {
	yaml := APIRouterConfigYAML(testConfig(), 9000)

	if !strings.Contains(yaml, "Host(`api.example.com`)") {
		t.Fatalf("expected the API host rule, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "http://127.0.0.1:9000") {
		t.Fatalf("expected the daemon's loopback address, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "certResolver: letsencrypt") {
		t.Fatalf("expected the letsencrypt cert resolver, got:\n%s", yaml)
	}
}

func TestWriteAPIRouterConfigWritesFile(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()

	if err := WriteAPIRouterConfig(cfg, 9000); err != nil {
		t.Fatalf("WriteAPIRouterConfig: %v", err)
	}

	data, err := os.ReadFile(cfg.DataDir + "/traefik-dynamic/api.yml")
	if err != nil {
		t.Fatalf("expected the config file to exist: %v", err)
	}
	if !strings.Contains(string(data), "api.example.com") {
		t.Fatalf("unexpected file content: %s", data)
	}
}

type fakeDocker struct {
	pulledRef   string
	createErr   error
	createdName string
	startedID   string
}

func (f *fakeDocker) PullImage(ctx context.Context, ref string) error {
	f.pulledRef = ref
	return nil
}
func (f *fakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createdName = opts.Name
	return "container-1", nil
}
func (f *fakeDocker) StartContainer(ctx context.Context, id string) error {
	f.startedID = id
	return nil
}

func TestEnsureCreatesAndStartsContainer(t *testing.T) {
	docker := &fakeDocker{}
	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.pulledRef != "registry:2" {
		t.Fatalf("expected image to be pulled, got %q", docker.pulledRef)
	}
	if docker.startedID != "container-1" {
		t.Fatalf("expected the created container to be started, got %q", docker.startedID)
	}
}

func TestEnsureIgnoresAlreadyExistsError(t *testing.T) {
	docker := &fakeDocker{createErr: errors.New("Conflict: container name already in use")}
	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err != nil {
		t.Fatalf("expected Ensure to swallow a create conflict, got %v", err)
	}
	if docker.startedID != "" {
		t.Fatalf("expected no start call when create failed, got %q", docker.startedID)
	}
}
