package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type webhookFakeDocker struct {
	running      bool
	pulledRef    string
	createCalled bool
	logsContent  string

	// pullGate, when non-nil, blocks PullImage until it is closed. Used
	// to prove the webhook responds before the deploy finishes.
	pullGate chan struct{}
	// pullCtxErr records whether the deploy's context was already
	// cancelled when the pull ran.
	pullCtxErr error
}

func (f *webhookFakeDocker) PullImage(ctx context.Context, ref string) error {
	if f.pullGate != nil {
		<-f.pullGate
	}
	if err := ctx.Err(); err != nil {
		f.pullCtxErr = err
		return err
	}
	f.pulledRef = ref
	return nil
}
func (f *webhookFakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.createCalled = true
	return "container-1", nil
}
func (f *webhookFakeDocker) StartContainer(ctx context.Context, id string) error  { return nil }
func (f *webhookFakeDocker) StopContainer(ctx context.Context, id string) error   { return nil }
func (f *webhookFakeDocker) RemoveContainer(ctx context.Context, id string) error { return nil }
func (f *webhookFakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return f.running, nil
}

func (f *webhookFakeDocker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logsContent)), nil
}

// dockerStdoutFrame wraps s in the Engine's 8-byte multiplexing header
// (stream=1 for stdout), the format ContainerLogs returns for a
// container created without a TTY.
func dockerStdoutFrame(s string) string {
	hdr := make([]byte, 8)
	hdr[0] = 1
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(s)))
	return string(hdr) + s
}

const registryNotificationPayload = `{
  "events": [
    {
      "action": "push",
      "target": {"repository": "myapp", "tag": "latest"}
    },
    {
      "action": "pull",
      "target": {"repository": "myapp", "tag": "latest"}
    },
    {
      "action": "push",
      "target": {"repository": "unknown-app", "tag": "latest"}
    }
  ]
}`

// webhookRequest builds a notification request carrying the header the
// registry is configured to send.
func webhookRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer secret-token")
	return req
}

// newWebhookTestServer wires a server over a file-backed store, since
// the background deploy goroutine and the test read it concurrently.
func newWebhookTestServer(t *testing.T, docker *webhookFakeDocker) (*Server, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "cubeship.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	return NewServer(s, orch, "secret-token", "registry.example.com"), s
}

func TestRegistryWebhookTriggersDeployForMatchedApp(t *testing.T) {
	docker := &webhookFakeDocker{running: true}
	srv, s := newWebhookTestServer(t, docker)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, webhookRequest(registryNotificationPayload))
	srv.deployWG.Wait()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// The daemon pulls over loopback, not from the public HTTPS host.
	if docker.pulledRef != "127.0.0.1:5000/myapp:latest" {
		t.Fatalf("expected deploy to pull 127.0.0.1:5000/myapp:latest, got %q", docker.pulledRef)
	}

	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" {
		t.Fatalf("expected app to be deployed, got container ID %q", app.ContainerID)
	}
	// The stored image keeps the public push path for display.
	if app.Image != "registry.example.com/myapp" {
		t.Fatalf("expected the app's image to stay the public push path, got %q", app.Image)
	}
}

func TestRegistryWebhookIgnoresUnknownRepository(t *testing.T) {
	docker := &webhookFakeDocker{running: true}
	srv, _ := newWebhookTestServer(t, docker)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, webhookRequest(`{
		"events": [{"action": "push", "target": {"repository": "unknown-app", "tag": "latest"}}]
	}`))
	srv.deployWG.Wait()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even for unmatched repository, got %d", rec.Code)
	}
	if docker.createCalled {
		t.Fatal("expected no container to be created for an unknown repository")
	}
}

func TestRegistryWebhookRejectsMissingToken(t *testing.T) {
	docker := &webhookFakeDocker{running: true}
	srv, s := newWebhookTestServer(t, docker)
	org, _ := s.CreateOrganization(context.Background(), "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(context.Background(), org.ID, "default", "Default")
	s.CreateApp(context.Background(), org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(registryNotificationPayload)))
	// deliberately no Authorization header, as a forged notification
	// from the internet would arrive
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	srv.deployWG.Wait()

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an unauthenticated notification, got %d", rec.Code)
	}
	if docker.createCalled {
		t.Fatal("a forged push notification must not trigger a deploy")
	}
}

func TestRegistryWebhookRejectsWrongToken(t *testing.T) {
	docker := &webhookFakeDocker{running: true}
	srv, s := newWebhookTestServer(t, docker)
	org, _ := s.CreateOrganization(context.Background(), "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(context.Background(), org.ID, "default", "Default")
	s.CreateApp(context.Background(), org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(registryNotificationPayload)))
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	srv.deployWG.Wait()

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if docker.createCalled {
		t.Fatal("a notification with the wrong token must not trigger a deploy")
	}
}

func TestRegistryWebhookAcksBeforeDeployFinishes(t *testing.T) {
	// The registry's notification client times out after 5s and retries
	// five times. The webhook must ack immediately and deploy detached
	// from the request, or every real deploy causes a retry storm and
	// its own cancellation.
	docker := &webhookFakeDocker{running: true, pullGate: make(chan struct{})}
	srv, s := newWebhookTestServer(t, docker)
	ctx := context.Background()
	org, _ := s.CreateOrganization(ctx, "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(ctx, org.ID, "default", "Default")
	s.CreateApp(ctx, org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req := webhookRequest(registryNotificationPayload).WithContext(reqCtx)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		srv.Router().ServeHTTP(rec, req)
		close(done)
	}()

	// The handler must return while the deploy is still blocked in the pull.
	<-done
	if rec.Code != http.StatusOK {
		t.Fatalf("expected an immediate 200, got %d", rec.Code)
	}

	// Simulate the registry's client hanging up, then let the deploy run.
	cancelReq()
	close(docker.pullGate)
	srv.deployWG.Wait()

	if docker.pullCtxErr != nil {
		t.Fatalf("the background deploy inherited the cancelled request context: %v", docker.pullCtxErr)
	}
	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" {
		t.Fatalf("expected the deploy to complete after the request ended, got container ID %q", app.ContainerID)
	}
}

// The webhook can fire twice in quick succession; the orchestrator's
// per-app lock must keep those deploys from racing.
func TestRegistryWebhookConcurrentNotificationsDoNotRace(t *testing.T) {
	docker := &countingWebhookDocker{}
	s, err := store.Open(filepath.Join(t.TempDir(), "cubeship.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	org, _ := s.CreateOrganization(context.Background(), "acme", "Acme Inc")
	orgProject, orgEnv, _ := s.CreateProjectWithDefaultEnvironment(context.Background(), org.ID, "default", "Default")
	s.CreateApp(context.Background(), org.ID, orgProject.ID, orgEnv.ID, "myapp", "myapp.example.com", "registry.example.com/myapp")

	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 3
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, webhookRequest(registryNotificationPayload))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	}
	srv.deployWG.Wait()

	created, removed := docker.counts()
	if created != 2 {
		t.Fatalf("expected two containers created across two pushes, got %d", created)
	}
	// The second deploy retires the first's container: no leak.
	if removed != 1 {
		t.Fatalf("expected exactly one container retired, got %d", removed)
	}
}

type countingWebhookDocker struct {
	mu        sync.Mutex
	createSeq int
	removed   int
}

func (f *countingWebhookDocker) PullImage(ctx context.Context, ref string) error { return nil }
func (f *countingWebhookDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createSeq++
	return "container-" + string(rune('0'+f.createSeq)), nil
}
func (f *countingWebhookDocker) StartContainer(ctx context.Context, id string) error { return nil }
func (f *countingWebhookDocker) StopContainer(ctx context.Context, id string) error  { return nil }
func (f *countingWebhookDocker) RemoveContainer(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed++
	return nil
}
func (f *countingWebhookDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return true, nil
}
func (f *countingWebhookDocker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *countingWebhookDocker) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createSeq, f.removed
}
