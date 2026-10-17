package bootstrap

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"cubeship/internal/platform/config"
	"cubeship/internal/platform/dockerx"
)

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
	const image = "sha256:registry2"
	if configHash(base, image) != configHash(base, image) {
		t.Fatal("the same options hashed differently")
	}

	// A tag is a moving name: the same options against a rebuilt image
	// are a different container, and this is what says so.
	if configHash(base, image) == configHash(base, "sha256:rebuilt") {
		t.Error("rebuilding the image under the same tag did not change the hash")
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
			if configHash(changed, image) == configHash(base, image) {
				t.Errorf("changing the %s did not change the hash", name)
			}
		})
	}

	// The hash lives in a label, so its own presence must not affect it —
	// otherwise every container would look changed on the next start.
	labelled := withConfigHash(base, "sha256:x")
	if configHash(labelled, "sha256:x") != configHash(base, "sha256:x") {
		t.Error("the config-hash label changed the hash of the options it describes")
	}
}

// Plain HTTP used to answer 404 for everything, since nothing was routed
// on :80.
func TestTraefikRedirectsHTTPToHTTPS(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), true, "admin@example.com", nil)

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

// Traefik v3 reads a whole request, body included, within 60s unless told
// otherwise, so a photo backup or a git push slower than a minute failed
// with a proxy error. Both entrypoints serve apps: :80 is the only one
// without a domain.
func TestTraefikLetsSlowUploadsFinish(t *testing.T) {
	for _, tls := range []bool{true, false} {
		opts := TraefikContainerOpts(testConfig(), tls, "", nil)
		for _, want := range []string{
			"--entrypoints.web.transport.respondingtimeouts.readtimeout=0",
			"--entrypoints.websecure.transport.respondingtimeouts.readtimeout=0",
		} {
			if !slices.Contains(opts.Cmd, want) {
				t.Errorf("tls=%v: missing %q; an upload slower than a minute is cut off", tls, want)
			}
		}
	}
}

// Until a domain is configured, Traefik must have no ACME resolver at
// all — there is nothing to get a certificate for — and must not
// redirect :80 to a port that cannot serve.
func TestTraefikWithoutADomainHasNoResolver(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), false, "", nil)

	for _, flag := range opts.Cmd {
		if strings.Contains(flag, "certificatesresolvers") {
			t.Errorf("a certificate resolver was configured with no domain: %q", flag)
		}
		if strings.Contains(flag, "redirections") {
			t.Errorf("plain HTTP is redirected to a port that cannot serve: %q", flag)
		}
	}
	if !slices.Contains(opts.Cmd, "--entrypoints.web.address=:80") {
		t.Error("apps have nowhere to be served without the plain entrypoint")
	}
}

// Setting a domain changes the options, which is what makes Ensure
// replace the container — a resolver cannot be added to a running
// Traefik.
func TestConfiguringTLSChangesTheTraefikContainer(t *testing.T) {
	const image = "sha256:same"
	without := configHash(TraefikContainerOpts(testConfig(), false, "", nil), image)
	with := configHash(TraefikContainerOpts(testConfig(), true, "admin@example.com", nil), image)

	if without == with {
		t.Fatal("configuring a domain left the container unchanged, so TLS would never take effect")
	}
}

// A contact address is a courtesy, not a condition: Let's Encrypt opens
// an account without one, so a domain alone must bring the resolver up,
// and the email flag appears only when there is an address to put in it.
func TestTLSNeedsNoContactAddress(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), true, "", nil)

	if !slices.Contains(opts.Cmd, "--certificatesresolvers.letsencrypt.acme.tlschallenge=true") {
		t.Error("a domain without a contact address got no certificate resolver")
	}
	for _, flag := range opts.Cmd {
		if strings.HasPrefix(flag, "--certificatesresolvers.letsencrypt.acme.email=") {
			t.Errorf("an empty contact address was passed to Traefik: %q", flag)
		}
	}
}

// Where each part reaches the others depends on whether the daemon is a
// container. Getting this backwards is not a compile error and not a
// test failure anywhere else: it is a daemon that starts, looks healthy,
// and cannot reach its own database.
func TestAddressesFollowWhereTheDaemonRuns(t *testing.T) {
	host := testConfig()
	host.InContainer = false

	contained := testConfig()
	contained.InContainer = true

	// The daemon's own reach into its infrastructure.
	if got := PostgresDSN(host, "pw"); !strings.Contains(got, "@127.0.0.1:") {
		t.Errorf("a host daemon connects to %q, want loopback", got)
	}
	if got := PostgresDSN(contained, "pw"); !strings.Contains(got, "@"+PostgresContainerName+":") {
		t.Errorf("a contained daemon connects to %q, want the container name", got)
	}
	if got := LocalRegistryAddress(host); got != "127.0.0.1:5000" {
		t.Errorf("a host daemon reads the registry at %q", got)
	}
	if got := LocalRegistryAddress(contained); got != RegistryContainerName+":5000" {
		t.Errorf("a contained daemon reads the registry at %q", got)
	}

	// And the reach back: a container calling the daemon cannot use
	// loopback, which is its own.
	if got := DaemonAddress(host, 3000); got != "host.docker.internal:3000" {
		t.Errorf("a container reaches a host daemon at %q", got)
	}
	if got := DaemonAddress(contained, 3000); got != DaemonContainerName+":3000" {
		t.Errorf("a container reaches a contained daemon at %q", got)
	}
}

// Traefik used to take the host's network namespace for one reason: to
// reach a daemon at 127.0.0.1. It costs more than it buys — not least
// that it does not work at all on Docker Desktop — and nothing needs it
// now.
func TestTraefikIsOnTheManagementNetwork(t *testing.T) {
	opts := TraefikContainerOpts(testConfig(), true, "admin@example.com", nil)

	if opts.HostNetwork {
		t.Error("Traefik is still on the host's network")
	}
	if opts.Network != dockerx.ManagementNetwork {
		t.Errorf("Traefik is on %q, want %q", opts.Network, dockerx.ManagementNetwork)
	}
	// It still has to answer the world on the two ports that matter.
	var published string
	for _, p := range opts.Ports {
		published += p + " "
	}
	for _, want := range []string{"80:80", "443:443"} {
		if !strings.Contains(published, want) {
			t.Errorf("Traefik does not publish %s (has %q)", want, published)
		}
	}

	// A daemon on the host is reached through the gateway, and Traefik
	// off the host's namespace cannot resolve that name without being
	// told it. Nothing fails at startup when this is missing: the API
	// simply stops being reachable through Traefik, which is not where
	// anyone looks.
	var hosts string
	for _, h := range opts.ExtraHosts {
		hosts += h + " "
	}
	if !strings.Contains(hosts, "host.docker.internal:host-gateway") {
		t.Errorf("Traefik cannot resolve a daemon on the host (ExtraHosts: %q)", hosts)
	}
}

// The registry is part of what Cubeship is, not something a domain
// switches on: the daemon pulls an app's own image from it, and a push
// from this host reaches it on loopback. A domain adds a name the rest
// of the world can push to, and nothing else.
func TestTheRegistryRunsWithoutADomain(t *testing.T) {
	cfg := testConfig()

	opts := RegistryContainerOpts(cfg, "", false, []byte("cert"))
	if opts.Name != RegistryContainerName || opts.Image == "" {
		t.Fatalf("no registry container without a domain: %+v", opts)
	}
	// A Traefik rule with an empty host matches nothing and is worth
	// less than not existing.
	for key := range opts.Labels {
		if strings.HasPrefix(key, "traefik.") {
			t.Errorf("a router was configured with no host to route: %v", opts.Labels)
			break
		}
	}
	// It is still reachable from this host, which is what a local push
	// and the daemon's own pulls use.
	var published string
	for _, p := range opts.Ports {
		published += p + " "
	}
	if !strings.Contains(published, "5000") {
		t.Errorf("the registry publishes %q, with nothing on 5000", published)
	}

	// With a domain it gains the router.
	withDomain := RegistryContainerOpts(cfg, "registry.example.com", true, []byte("cert"))
	if len(withDomain.Labels) == 0 {
		t.Error("a registry with a domain has no Traefik labels")
	}
}

// A client is told where to fetch a token. Without a domain there is no
// public name to send it to, and the only address that works is this
// host's own — enough for a push from here, which is the case that
// exists before a domain.
func TestTheTokenRealmFallsBackToThisHost(t *testing.T) {
	withDomain := RegistryConfigYAML("api.example.com", "http://daemon/hooks", "tok3n")
	if !strings.Contains(withDomain, "realm: https://api.example.com/v2/token") {
		t.Errorf("the realm is not the public API:\n%s", withDomain)
	}

	without := RegistryConfigYAML("", "http://daemon/hooks", "tok3n")
	if !strings.Contains(without, "realm: http://127.0.0.1:3000/v2/token") {
		t.Errorf("the realm has no working fallback:\n%s", without)
	}
}

// An image built on this host exists in no registry, so pulling it fails
// — and pulling it at all is the bug: `install.sh --local` builds the
// daemon and the dashboard here, and Ensure used to pull unconditionally,
// which meant the dashboard's container never started on a local install.
func TestEnsureDoesNotPullAnImageItAlreadyHas(t *testing.T) {
	docker := &fakeDocker{
		localImages: map[string]string{"cubeship:local": "sha256:aaa"},
		pullErr:     errors.New("pull access denied: repository does not exist"),
	}

	err := Ensure(context.Background(), docker, dockerx.ContainerOpts{
		Name:  "cubeship-frontend",
		Image: "cubeship:local",
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.pulledRef != "" {
		t.Errorf("pulled %q, which is already on the host", docker.pulledRef)
	}
	if docker.createdName != "cubeship-frontend" {
		t.Errorf("created %q, want the container to have been created", docker.createdName)
	}
}

// The other half: an image that is genuinely absent is still fetched.
func TestEnsurePullsAnImageItDoesNotHave(t *testing.T) {
	docker := &fakeDocker{}

	if err := Ensure(context.Background(), docker, dockerx.ContainerOpts{
		Name:  "cubeship-postgres",
		Image: "postgres:16-alpine",
	}); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if docker.pulledRef != "postgres:16-alpine" {
		t.Errorf("pulled %q, want postgres:16-alpine", docker.pulledRef)
	}
}

// A rebuilt image under the same tag is a different image, and the
// container running the old one has to go.
//
// This is what `install.sh --local` does on every install: it rebuilds
// `cubeship:local`, whose ContainerOpts are identical
// every time. A fingerprint taken from the options alone said nothing
// had changed, so the box kept running the previous build — which looked
// exactly like a cache and was not one.
func TestEnsureReplacesAContainerWhoseImageWasRebuilt(t *testing.T) {
	opts := dockerx.ContainerOpts{Name: "cubeship-frontend", Image: "cubeship:local"}

	// What the container was created from, the first time round.
	first := &fakeDocker{localImages: map[string]string{opts.Image: "sha256:old"}}
	if err := Ensure(context.Background(), first, opts); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	stamped := first.createdLabels[ConfigHashLabel]
	if stamped == "" {
		t.Fatal("the container was created without a config hash")
	}

	// The same tag, rebuilt: the container exists, is running, and
	// carries the old fingerprint.
	rebuilt := &fakeDocker{
		localImages:    map[string]string{opts.Image: "sha256:new"},
		inspectID:      "old-container",
		inspectRunning: true,
		inspectLabels:  map[string]string{ConfigHashLabel: stamped},
		inspectErr:     nil,
	}
	if err := Ensure(context.Background(), rebuilt, opts); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if len(rebuilt.removed) == 0 {
		t.Error("the container running the previous build was left in place")
	}
	if rebuilt.createdName != opts.Name {
		t.Error("no replacement container was created")
	}
}

// A changed trust root has to replace the registry container, not just
// rewrite the file it already read.
//
// The registry loads its rootcertbundle once, at startup, and accepts
// only that exact certificate. Nothing else about the container changes
// when the certificate does — same image, same binds, same path — so
// without this the daemon wrote a new root to disk, Ensure saw an
// unchanged container, and the registry went on refusing every token
// the daemon signed.
func TestANewTokenCertificateReplacesTheRegistry(t *testing.T) {
	cfg := testConfig()
	before := RegistryContainerOpts(cfg, "registry.example.com", true, []byte("first"))
	after := RegistryContainerOpts(cfg, "registry.example.com", true, []byte("second"))

	const image = "sha256:registry2"
	if configHash(before, image) == configHash(after, image) {
		t.Error("a different trust root left the container looking unchanged, so the registry keeps the old one")
	}
	if before.Labels[TokenCertLabel] == "" {
		t.Error("the container does not record which trust root it was started with")
	}
}

// On the host the daemon is not root, and the socket buildkitd creates
// as root would be one it cannot open. In its container it is root and
// nothing needs saying.
func TestBuildKitSocketIsReachableByAHostDaemon(t *testing.T) {
	cfg := testConfig()
	cfg.InContainer = false
	host := BuildKitContainerOpts(cfg)
	if !slices.Contains(host.Cmd, "--group") {
		t.Errorf("a host daemon's buildkitd does not hand its socket to the daemon's group: %v", host.Cmd)
	}

	cfg.InContainer = true
	if got := BuildKitContainerOpts(cfg).Cmd; slices.Contains(got, "--group") {
		t.Errorf("a containerised daemon passes buildkitd flags it does not need: %v", got)
	}
}

// The builder's layer cache is what filled the disk: buildkitd's default
// ceiling is 80% of it, so nothing was ever collected. Every container the
// daemon starts for it carries an explicit, smaller one.
func TestBuildKitCacheHasACeiling(t *testing.T) {
	for _, inContainer := range []bool{true, false} {
		cfg := testConfig()
		cfg.InContainer = inContainer
		var found string
		for _, arg := range BuildKitContainerOpts(cfg).Cmd {
			if v, ok := strings.CutPrefix(arg, "--oci-worker-gc-keepstorage="); ok {
				found = v
			}
		}
		if found == "" {
			t.Errorf("InContainer=%v: buildkitd runs with its default cache limit", inContainer)
		}
	}
}

func TestBuildKitCacheLimitFollowsTheDisk(t *testing.T) {
	const gb = int64(1e9)
	for _, tc := range []struct {
		name  string
		total int64
		want  string // Reserved,Free,Maximum in MB
	}{
		{"a disk that cannot be read keeps the fixed ceiling", 0, "10000,0,20000"},
		{"a large disk is capped at 20 GB", 155 * gb, "10000,31000,20000"},
		{"a small disk gives the cache a quarter", 40 * gb, "5000,8000,10000"},
	} {
		if got := buildKitGCKeepStorage(tc.total); got != tc.want {
			t.Errorf("%s: buildKitGCKeepStorage(%d) = %q, want %q", tc.name, tc.total, got, tc.want)
		}
	}
}

// TestNoInfrastructureContainerIsCapped guards the one thing configHash
// deliberately does not look at.
//
// A ceiling is left out of the fingerprint because the Engine can change
// it on a running container, so a new one is not a reason to replace
// anything — but Ensure does not apply one either, and nothing here has
// one to apply. Giving one of these a ceiling means teaching Ensure to
// set it; this is the line that says so.
func TestNoInfrastructureContainerIsCapped(t *testing.T) {
	cfg := &config.Config{DataDir: "/var/lib/cubeship"}
	for _, tc := range []struct {
		name string
		opts dockerx.ContainerOpts
	}{
		{"postgres", PostgresContainerOpts(cfg, "pw")},
		{"registry", RegistryContainerOpts(cfg, "registry.example.com", true, []byte("cert"))},
		{"buildkit", BuildKitContainerOpts(cfg)},
		{"frontend", FrontendContainerOpts("cubeship:v1")},
		{"traefik", TraefikContainerOpts(cfg, true, "ops@example.com", nil)},
	} {
		if !tc.opts.Resources.Unlimited() {
			t.Errorf("%s carries a ceiling Ensure would never apply: %+v", tc.name, tc.opts.Resources)
		}
	}
}

// Where the daemon reads the registry and where the Engine pulls from
// are two questions, and for a containerized daemon they have different
// answers.
//
// They were one value, and the Engine got the container's name. An
// image reference is resolved by the Engine — the host's daemon, on no
// user-defined network — and container names live in Docker's embedded
// DNS, which only containers on that network can ask. So every deploy
// of an app on this instance's own registry failed with `lookup
// cubeship-registry on 127.0.0.53:53: server misbehaving`, which reads
// as a broken registry and is a name that was never going to resolve
// where it was sent.
//
// Nothing but a real Engine can catch that, which is why the property
// worth pinning is the one above it: the address that becomes an image
// reference is never a container's name.
func TestTheEngineIsGivenAnAddressItCanResolve(t *testing.T) {
	pull := PullRegistryAddress()
	if !strings.HasPrefix(pull, "127.0.0.1:") {
		t.Errorf("the Engine is told to pull from %q, which is not the host's loopback", pull)
	}
	if strings.Contains(pull, RegistryContainerName) {
		t.Errorf("the Engine is told to pull from %q, a name only containers on the bridge resolve", pull)
	}

	// And it does not follow the daemon, which is the whole distinction:
	// the published port is on the host either way.
	contained := testConfig()
	contained.InContainer = true
	if pull == LocalRegistryAddress(contained) {
		t.Error("a contained daemon hands the Engine its own address for the registry")
	}
	host := testConfig()
	host.InContainer = false
	if pull != LocalRegistryAddress(host) {
		t.Errorf("on a host daemon the two disagree: %q and %q", pull, LocalRegistryAddress(host))
	}
}
