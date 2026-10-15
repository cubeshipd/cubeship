package objectstore

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"cubeship/internal/credential"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// countingPorts is a PortsChanged that counts, and fails when told to.
type countingPorts struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *countingPorts) SyncPublished(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.err
}

func (c *countingPorts) take() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.calls
	c.calls = 0
	return n
}

// exposeFakeDocker is enough of DockerAPI to provision with no Docker:
// everything succeeds and the container is running on the first look.
type exposeFakeDocker struct{}

func (exposeFakeDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error {
	return nil
}
func (exposeFakeDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "fake-container", nil
}
func (exposeFakeDocker) StartContainer(context.Context, string) error  { return nil }
func (exposeFakeDocker) StopContainer(context.Context, string) error   { return nil }
func (exposeFakeDocker) RemoveContainer(context.Context, string) error { return nil }
func (exposeFakeDocker) SetResources(context.Context, string, dockerx.Resources) error {
	return nil
}
func (exposeFakeDocker) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (exposeFakeDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

var exposeTestAdmin = &user.User{Username: "admin", Role: user.RoleAdmin}

// newExposeFixture is a Service over a throwaway database, told about
// port changes through ports when that is not nil.
func newExposeFixture(t *testing.T, ports PortsChanged) *Service {
	t.Helper()
	db := dbtest.New(t)
	prov := NewProvisioner(db, exposeFakeDocker{}, t.TempDir())
	prov.ReadyAttempts = 1
	svc := NewService(db, credential.NewService(db), nil, prov,
		settings.NewService(db), metrics.NewService(db))
	if ports != nil {
		svc.SetPortsChanged(ports)
	}
	t.Cleanup(svc.WaitForProvisioning)
	return svc
}

func TestCreatingAnExposedStoreSyncsTheFirewallOnce(t *testing.T) {
	ports := &countingPorts{}
	svc := newExposeFixture(t, ports)
	ctx := context.Background()

	port := PortRangeStart
	if _, err := svc.Create(ctx, exposeTestAdmin, ManagedSpec{Slug: "media", Expose: &port}); err != nil {
		t.Fatalf("create exposed: %v", err)
	}
	if _, err := svc.Create(ctx, exposeTestAdmin, ManagedSpec{Slug: "quiet"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	svc.WaitForProvisioning()
	if n := ports.take(); n != 1 {
		t.Errorf("synced %d times for one exposed create and one not", n)
	}
}

// Every change to exposure is one sync: exposing, moving to another
// port, unexposing, and deleting one that is exposed. Asking for what is
// already so changes nothing and says nothing.
func TestEachChangeToExposureSyncsTheFirewallOnce(t *testing.T) {
	ports := &countingPorts{}
	svc := newExposeFixture(t, ports)
	ctx := context.Background()

	store, err := svc.Create(ctx, exposeTestAdmin, ManagedSpec{Slug: "media"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	svc.WaitForProvisioning()

	steps := []struct {
		name string
		do   func() error
		want int
	}{
		{"expose", func() error { _, err := svc.Expose(ctx, exposeTestAdmin, store.Slug, PortRangeStart); return err }, 1},
		{"expose on the same port", func() error { _, err := svc.Expose(ctx, exposeTestAdmin, store.Slug, PortRangeStart); return err }, 0},
		{"move to another port", func() error { _, err := svc.Expose(ctx, exposeTestAdmin, store.Slug, PortRangeStart+1); return err }, 1},
		{"unexpose", func() error { _, err := svc.Unexpose(ctx, exposeTestAdmin, store.Slug); return err }, 1},
		{"unexpose again", func() error { _, err := svc.Unexpose(ctx, exposeTestAdmin, store.Slug); return err }, 0},
		{"expose to delete", func() error { _, err := svc.Expose(ctx, exposeTestAdmin, store.Slug, 0); return err }, 1},
		{"delete", func() error { _, err := svc.Delete(ctx, exposeTestAdmin, store.Slug); return err }, 1},
	}
	for _, step := range steps {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		svc.WaitForProvisioning()
		if n := ports.take(); n != step.want {
			t.Errorf("%s: synced %d times, want %d", step.name, n, step.want)
		}
	}
}

// A linked store has no container and publishes nothing, so neither
// linking nor deleting one reaches the firewall.
func TestALinkedStoreNeverSyncsTheFirewall(t *testing.T) {
	ports := &countingPorts{}
	svc := newExposeFixture(t, ports)
	ctx := context.Background()

	store, err := svc.Link(ctx, exposeTestAdmin, LinkSpec{
		Slug: "ext", Provider: ProviderGeneric, Endpoint: "s3.example.com",
	}, &NewLogin{AccessKey: "AKIAEXAMPLE", SecretKey: "supersecretkey"})
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := svc.Delete(ctx, exposeTestAdmin, store.Slug); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := ports.take(); n != 0 {
		t.Errorf("a linked store synced %d times", n)
	}
}

// The expose has happened whatever the host said, and with nothing to
// tell it happens all the same.
func TestTheFirewallNeverFailsAnExpose(t *testing.T) {
	for name, ports := range map[string]PortsChanged{
		"nil":     nil,
		"failing": &countingPorts{err: errors.New("no host")},
	} {
		t.Run(name, func(t *testing.T) {
			svc := newExposeFixture(t, ports)
			ctx := context.Background()
			store, err := svc.Create(ctx, exposeTestAdmin, ManagedSpec{Slug: "media"})
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			svc.WaitForProvisioning()
			if _, err := svc.Expose(ctx, exposeTestAdmin, store.Slug, 0); err != nil {
				t.Fatalf("expose: %v", err)
			}
			svc.WaitForProvisioning()
		})
	}
}
