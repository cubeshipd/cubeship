package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/regauth"
)

func testConfig() *config.Config {
	return &config.Config{DataDir: "/var/lib/cubeship"}
}

func TestRegistryContainerOptsRoutesThroughTraefik(t *testing.T) {
	opts := RegistryContainerOpts(testConfig(), "registry.example.com", true)

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

	wantBinds := []string{
		"/var/lib/cubeship/registry-config.yml:/etc/docker/registry/config.yml:ro",
		"/var/lib/cubeship/registry-token.crt:/etc/docker/registry/token.crt:ro",
		"/var/lib/cubeship/registry-data:/var/lib/registry",
	}
	if len(opts.Binds) != len(wantBinds) {
		t.Fatalf("expected config.yml + token cert + persistent storage binds, got %v", opts.Binds)
	}
	for i, want := range wantBinds {
		if opts.Binds[i] != want {
			t.Fatalf("bind %d: expected %q, got %q", i, want, opts.Binds[i])
		}
	}

	if len(opts.ExtraHosts) != 1 || opts.ExtraHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("expected host.docker.internal to resolve to the host gateway so the container can reach a notifyURL on the host, got %v", opts.ExtraHosts)
	}
}

func TestRegistryConfigYAMLIncludesNotificationEndpoint(t *testing.T) {
	yaml := RegistryConfigYAML("api.example.com", "http://host.docker.internal:9000/hooks/registry", "tok3n")

	if !strings.Contains(yaml, "url: http://host.docker.internal:9000/hooks/registry") {
		t.Fatalf("expected the notify URL in the endpoint config, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "notifications:") || !strings.Contains(yaml, "endpoints:") {
		t.Fatalf("expected a notifications.endpoints section, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "addr: :5000") {
		t.Fatalf("expected the base registry http config to be present (this replaces, not merges with, the image default), got:\n%s", yaml)
	}
}

func TestRegistryConfigYAMLRequiresTokenAuth(t *testing.T) {
	yaml := RegistryConfigYAML("api.example.com", "http://host.docker.internal:9000/hooks/registry", "tok3n")

	if !strings.Contains(yaml, "auth:") || !strings.Contains(yaml, "token:") {
		t.Fatalf("expected a token auth section — an anonymous-push registry is remote code execution, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "realm: https://api.example.com/v2/token") {
		t.Fatalf("expected the realm to point at the daemon's own token endpoint, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "service: "+regauth.TokenService) {
		t.Fatalf("expected the service to match regauth.TokenService, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "issuer: "+regauth.TokenIssuer) {
		t.Fatalf("expected the issuer to match regauth.TokenIssuer, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "rootcertbundle: /etc/docker/registry/token.crt") {
		t.Fatalf("expected the auth section to point at the mounted token cert, got:\n%s", yaml)
	}
}

func TestRegistryConfigYAMLAuthenticatesTheWebhook(t *testing.T) {
	yaml := RegistryConfigYAML("api.example.com", "http://host.docker.internal:9000/hooks/registry", "tok3n")

	if !strings.Contains(yaml, "Authorization: [Bearer tok3n]") {
		t.Fatalf("expected the notification endpoint to send the daemon's bearer token, got:\n%s", yaml)
	}
}

func TestWriteRegistryConfigWritesFileAndStorageDir(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()

	if err := WriteRegistryConfig(cfg, "api.example.com", "http://host.docker.internal:9000/hooks/registry", "tok3n"); err != nil {
		t.Fatalf("WriteRegistryConfig: %v", err)
	}

	data, err := os.ReadFile(cfg.DataDir + "/registry-config.yml")
	if err != nil {
		t.Fatalf("expected the config file to exist: %v", err)
	}
	if !strings.Contains(string(data), "host.docker.internal:9000") {
		t.Fatalf("unexpected file content: %s", data)
	}

	info, err := os.Stat(cfg.DataDir + "/registry-data")
	if err != nil || !info.IsDir() {
		t.Fatalf("expected the registry storage dir to be created: %v", err)
	}
}

func TestWriteRegistryTokenCertWritesFile(t *testing.T) {
	cfg := testConfig()
	cfg.DataDir = t.TempDir()

	key, err := regauth.LoadOrCreateKeyPair(cfg.DataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyPair: %v", err)
	}
	certPEM, err := regauth.SelfSignedCert(key, "cubeship")
	if err != nil {
		t.Fatalf("SelfSignedCert: %v", err)
	}

	if err := WriteRegistryTokenCert(cfg, certPEM); err != nil {
		t.Fatalf("WriteRegistryTokenCert: %v", err)
	}

	data, err := os.ReadFile(cfg.DataDir + "/registry-token.crt")
	if err != nil {
		t.Fatalf("expected the cert file to exist: %v", err)
	}
	if string(data) != string(certPEM) {
		t.Fatal("expected the written file to match the cert exactly")
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
	yaml := APIRouterConfigYAML("api.example.com", 9000)

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

	if err := WriteAPIRouterConfig(cfg, "api.example.com", 9000); err != nil {
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
	pulledRef string
	createErr error
	// createdName stays "" until CreateContainer is actually called, so
	// tests can assert Ensure did *not* try to recreate an existing
	// container.
	createdName string
	startedID   string

	// inspectID/inspectRunning/inspectLabels describe an existing
	// container; inspectErr (default: dockerx.ErrContainerNotFound)
	// describes its absence or a broken daemon.
	inspectID      string
	inspectRunning bool
	inspectLabels  map[string]string
	inspectErr     error
	inspectedName  string

	stopped   []string
	removed   []string
	stopErr   error
	removeErr error
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{inspectErr: dockerx.ErrContainerNotFound}
}

func (f *fakeDocker) PullImage(ctx context.Context, ref string, _ *dockerx.RegistryAuth) error {
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
func (f *fakeDocker) StopContainer(ctx context.Context, id string) error {
	f.stopped = append(f.stopped, id)
	return f.stopErr
}

func (f *fakeDocker) RemoveContainer(ctx context.Context, id string) error {
	f.removed = append(f.removed, id)
	return f.removeErr
}

func (f *fakeDocker) InspectContainerByName(ctx context.Context, name string) (dockerx.ContainerInfo, error) {
	f.inspectedName = name
	if f.inspectErr != nil {
		return dockerx.ContainerInfo{}, f.inspectErr
	}
	return dockerx.ContainerInfo{
		ID: f.inspectID, Running: f.inspectRunning, Labels: f.inspectLabels,
	}, nil
}

// matching makes the fake describe an existing container created from
// exactly these options, so Ensure sees nothing to change.
func (f *fakeDocker) matching(opts dockerx.ContainerOpts) *fakeDocker {
	f.inspectErr = nil
	f.inspectID = "existing-container"
	f.inspectLabels = map[string]string{ConfigHashLabel: configHash(opts)}
	return f
}

func TestEnsureCreatesAndStartsContainer(t *testing.T) {
	docker := newFakeDocker()
	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.inspectedName != "cubeship-registry" {
		t.Fatalf("expected Ensure to look for an existing container first, got %q", docker.inspectedName)
	}
	if docker.pulledRef != "registry:2" {
		t.Fatalf("expected image to be pulled, got %q", docker.pulledRef)
	}
	if docker.startedID != "container-1" {
		t.Fatalf("expected the created container to be started, got %q", docker.startedID)
	}
}

func TestEnsureLeavesRunningContainerAlone(t *testing.T) {
	opts := dockerx.ContainerOpts{Name: "cubeship-traefik", Image: "traefik:v3.1"}
	docker := newFakeDocker().matching(opts)
	docker.inspectRunning = true

	if err := Ensure(context.Background(), docker, opts); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.createdName != "" {
		t.Fatalf("expected no create for an already-running container, got %q", docker.createdName)
	}
	if docker.startedID != "" {
		t.Fatalf("expected no start for an already-running container, got %q", docker.startedID)
	}
	if docker.pulledRef != "" {
		t.Fatalf("expected no pull for an already-running container, got %q", docker.pulledRef)
	}
}

func TestEnsureStartsExistingStoppedContainer(t *testing.T) {
	// After a host reboot cubeship-traefik exists but is exited; the
	// daemon must start it, not report success with no proxy running.
	opts := dockerx.ContainerOpts{Name: "cubeship-traefik", Image: "traefik:v3.1"}
	docker := newFakeDocker().matching(opts)
	docker.inspectRunning = false

	if err := Ensure(context.Background(), docker, opts); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.startedID != "existing-container" {
		t.Fatalf("expected the existing stopped container to be started, got %q", docker.startedID)
	}
	if docker.createdName != "" {
		t.Fatalf("expected no create for an existing container, got %q", docker.createdName)
	}
}

func TestEnsureIgnoresConcurrentNameConflict(t *testing.T) {
	docker := newFakeDocker()
	docker.createErr = errors.New("Conflict: container name already in use")

	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err != nil {
		t.Fatalf("expected Ensure to swallow a create name conflict, got %v", err)
	}
	if docker.startedID != "" {
		t.Fatalf("expected no start call when create failed, got %q", docker.startedID)
	}
}

func TestEnsureReturnsRealCreateErrors(t *testing.T) {
	docker := newFakeDocker()
	docker.createErr = errors.New("invalid mount config: bind source path does not exist")

	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err == nil {
		t.Fatal("expected a genuine create failure to be returned, not assumed to mean already-exists")
	}
	if !strings.Contains(err.Error(), "bind source path does not exist") {
		t.Fatalf("expected the underlying error to be wrapped, got %v", err)
	}
}

func TestEnsureReturnsInspectErrors(t *testing.T) {
	docker := newFakeDocker()
	docker.inspectErr = errors.New("cannot connect to the docker daemon")

	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"})
	if err == nil {
		t.Fatal("expected a broken docker connection to be reported")
	}
	if docker.createdName != "" {
		t.Fatalf("expected no create attempt after an inspect failure, got %q", docker.createdName)
	}
}

func TestPostgresDSNEscapesThePassword(t *testing.T) {
	// Generated passwords are hex, but an operator-supplied one need not
	// be: a "/" or "@" in a raw password would be read as the end of the
	// credentials and point the daemon at the wrong host.
	dsn := PostgresDSN("p@ss/word")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("PostgresDSN produced an unparseable URL %q: %v", dsn, err)
	}
	if u.Host != fmt.Sprintf("127.0.0.1:%d", PostgresPort) {
		t.Errorf("host is %q, want 127.0.0.1:%d", u.Host, PostgresPort)
	}
	pw, _ := u.User.Password()
	if pw != "p@ss/word" {
		t.Errorf("password round-tripped as %q, want %q", pw, "p@ss/word")
	}
}

// The database must outlive its container: without a host bind mount,
// recreating cubeship-postgres would destroy every app, user and key.
func TestPostgresContainerPersistsItsDataOnTheHost(t *testing.T) {
	cfg := &config.Config{DataDir: "/var/lib/cubeship"}
	opts := PostgresContainerOpts(cfg, "secret")

	wantBind := "/var/lib/cubeship/postgres:/var/lib/postgresql/data"
	if !slices.Contains(opts.Binds, wantBind) {
		t.Errorf("binds %v do not include %q", opts.Binds, wantBind)
	}
	// Loopback only: the daemon is a host process, but nothing off this
	// machine should reach the database.
	wantPort := fmt.Sprintf("127.0.0.1:%d:5432", PostgresPort)
	if !slices.Contains(opts.Ports, wantPort) {
		t.Errorf("ports %v do not include %q", opts.Ports, wantPort)
	}
	if !slices.Contains(opts.Env, "POSTGRES_PASSWORD=secret") {
		t.Errorf("env %v does not carry the password", opts.Env)
	}
}

// The reason this exists: Docker cannot change an existing container's
// image, binds, ports or environment, so a release that changes any of
// them used to need a manual `docker rm -f` — and silently ran the old
// settings until someone did it.
func TestEnsureReplacesAContainerWhoseConfigurationChanged(t *testing.T) {
	before := dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"}
	after := before
	after.Binds = []string{"/var/lib/cubeship/registry-data:/var/lib/registry"}

	docker := newFakeDocker().matching(before)
	docker.inspectRunning = true

	if err := Ensure(context.Background(), docker, after); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if !slices.Contains(docker.removed, "existing-container") {
		t.Errorf("the outdated container was not removed: %v", docker.removed)
	}
	if docker.createdName != "cubeship-registry" {
		t.Errorf("no replacement was created, got %q", docker.createdName)
	}
	if docker.startedID != "container-1" {
		t.Errorf("the replacement was not started, got %q", docker.startedID)
	}
}

// A container created before the config-hash label existed carries no
// hash, so it is replaced once — which performs the manual step the
// upgrade notes used to ask for.
func TestEnsureReplacesAContainerWithNoConfigHash(t *testing.T) {
	opts := dockerx.ContainerOpts{Name: "cubeship-traefik", Image: "traefik:v3.1"}

	docker := newFakeDocker()
	docker.inspectErr = nil
	docker.inspectID = "legacy-container"
	docker.inspectRunning = true
	docker.inspectLabels = map[string]string{"traefik.enable": "true"}

	if err := Ensure(context.Background(), docker, opts); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !slices.Contains(docker.removed, "legacy-container") {
		t.Errorf("the unlabelled container was not replaced: %v", docker.removed)
	}
}

// If the replacement cannot be removed, creating one would collide on
// the name and be mistaken for a concurrent create — leaving the stale
// container running and the daemon reporting success.
func TestEnsureFailsWhenTheOutdatedContainerCannotBeRemoved(t *testing.T) {
	before := dockerx.ContainerOpts{Name: "cubeship-registry", Image: "registry:2"}
	after := before
	after.Image = "registry:3"

	docker := newFakeDocker().matching(before)
	docker.inspectRunning = true
	docker.removeErr = errors.New("device busy")

	if err := Ensure(context.Background(), docker, after); err == nil {
		t.Fatal("expected Ensure to fail when it cannot remove the outdated container")
	}
	if docker.createdName != "" {
		t.Errorf("a replacement was created anyway: %q", docker.createdName)
	}
}

// The label records the options, so identical options must hash
// identically and any change must not.
func TestConfigHashTracksTheOptions(t *testing.T) {
	base := dockerx.ContainerOpts{
		Name:   "cubeship-registry",
		Image:  "registry:2",
		Env:    []string{"A=1"},
		Binds:  []string{"/data:/var/lib/registry"},
		Labels: map[string]string{"traefik.enable": "true"},
	}
	if configHash(base) != configHash(base) {
		t.Fatal("the same options hashed differently")
	}

	for name, mutate := range map[string]func(o *dockerx.ContainerOpts){
		"image":   func(o *dockerx.ContainerOpts) { o.Image = "registry:3" },
		"env":     func(o *dockerx.ContainerOpts) { o.Env = []string{"A=2"} },
		"binds":   func(o *dockerx.ContainerOpts) { o.Binds = []string{"/other:/var/lib/registry"} },
		"ports":   func(o *dockerx.ContainerOpts) { o.Ports = []string{"127.0.0.1:5000:5000"} },
		"labels":  func(o *dockerx.ContainerOpts) { o.Labels = map[string]string{"traefik.enable": "false"} },
		"network": func(o *dockerx.ContainerOpts) { o.Network = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if configHash(changed) == configHash(base) {
				t.Errorf("changing the %s did not change the hash", name)
			}
		})
	}

	// The hash lives in a label, so its own presence must not affect it —
	// otherwise every container would look changed on the next start.
	labelled := withConfigHash(base)
	if configHash(labelled) != configHash(base) {
		t.Error("the config-hash label changed the hash of the options it describes")
	}
}

// Plain HTTP used to answer 404 for everything, since nothing was routed
// on :80.
func TestTraefikRedirectsHTTPToHTTPS(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), "admin@example.com")

	for _, want := range []string{
		"--entrypoints.web.http.redirections.entryPoint.to=websecure",
		"--entrypoints.web.http.redirections.entryPoint.scheme=https",
	} {
		if !slices.Contains(opts.Cmd, want) {
			t.Errorf("missing %q; plain HTTP would 404 instead of redirecting", want)
		}
	}

	// The redirect is only safe because certificates are issued over
	// TLS-ALPN on :443, not the HTTP challenge on :80.
	if !slices.Contains(opts.Cmd, "--certificatesresolvers.letsencrypt.acme.tlschallenge=true") {
		t.Error("the ACME resolver is no longer using the TLS challenge; redirecting :80 would break issuance")
	}
}

// Until a contact address is configured, Traefik must have no ACME
// resolver at all — Let's Encrypt will not register an account without
// one — and must not redirect :80 to a port that cannot serve.
func TestTraefikWithoutAnACMEEmailHasNoResolver(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), "")

	for _, flag := range opts.Cmd {
		if strings.Contains(flag, "certificatesresolvers") {
			t.Errorf("a certificate resolver was configured with no contact address: %q", flag)
		}
		if strings.Contains(flag, "redirections") {
			t.Errorf("plain HTTP is redirected to a port that cannot serve: %q", flag)
		}
	}
	if !slices.Contains(opts.Cmd, "--entrypoints.web.address=:80") {
		t.Error("apps have nowhere to be served without the plain entrypoint")
	}
}

// Adding the contact address changes the options, which is what makes
// Ensure replace the container — a resolver cannot be added to a running
// Traefik.
func TestConfiguringTLSChangesTheTraefikContainer(t *testing.T) {
	without := configHash(TraefikContainerOpts(testConfig(), ""))
	with := configHash(TraefikContainerOpts(testConfig(), "admin@example.com"))

	if without == with {
		t.Fatal("configuring a contact address left the container unchanged, so TLS would never take effect")
	}
}
