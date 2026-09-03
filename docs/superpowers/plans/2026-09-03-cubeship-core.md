# Cubeship Core Deploy Engine — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go daemon + CLI that deploys a containerized app to a VPS by
pushing an image to a built-in registry, auto-routes it through Traefik
with HTTPS, and redeploys with zero downtime on every new image push.

**Architecture:** A single daemon process owns the Docker socket and a
SQLite database. It manages three container groups (registry, Traefik,
app containers) purely through the Docker API — never shells out to
`docker`. The CLI is a thin HTTP client talking to the daemon's API.
Deploy is triggered by the registry's push notification webhook, and
reachable manually through the same API path.

**Tech Stack:** Go 1.23+, `github.com/docker/docker/client` (Docker
Engine API), `modernc.org/sqlite` (cgo-free SQLite driver),
`github.com/spf13/cobra` (CLI), standard library `net/http` (daemon API,
no framework).

**Spec:** [docs/superpowers/specs/2026-09-03-cubeship-core-design.md](../specs/2026-09-03-cubeship-core-design.md)

## Global Constraints

- Language is Go (not Rust) — see spec "Decisions carried from brainstorming".
- The daemon never writes Traefik config files; routing is 100% via
  Docker labels on app containers.
- The registry is the stock `distribution/distribution` image, run as a
  managed container — never a from-scratch OCI implementation.
- A failed deploy must never stop or remove a healthy running container.
- Daemon's own API is reached over HTTPS through Traefik, authenticated
  with a bearer token generated at install time.

---

## File Structure

```
cubeship/
  go.mod
  cmd/
    cubeshipd/main.go       # daemon entrypoint
    cubeship/main.go        # CLI entrypoint
    cubeship/app.go         # CLI `app` subcommands
    cubeship/login.go       # CLI `login` / `registry login`
  internal/
    store/
      store.go              # SQLite open + schema migration
      apps.go                # App CRUD
      deployments.go         # Deployment history CRUD
    dockerx/
      client.go             # Docker Engine API wrapper
      containers.go          # create/start/stop/remove/health-check
    traefik/
      labels.go             # Docker label builder for routing
    deploy/
      orchestrator.go       # pull -> start -> health-check -> swap -> record
    api/
      server.go             # router + auth middleware
      apps_handlers.go       # POST/GET /apps
      deploy_handlers.go     # POST /apps/{name}/deploy, PUT env, GET logs
      webhook_handler.go     # POST /hooks/registry
    reconcile/
      reconcile.go           # startup state reconciliation
    config/
      config.go             # daemon config from env vars
    apiclient/
      client.go             # CLI's HTTP client for the daemon API
    clicreds/
      clicreds.go           # CLI credentials file + registry host derivation
  test/
    integration/
      testapp/Dockerfile    # tiny fixture image the e2e test pushes
      deploy_test.go        # push image -> app reachable via Traefik
```

## Task Right-Sizing Note

Tasks 1-11 build and test the daemon in isolation (mocked/fake Docker +
real SQLite). Task 12 wires the daemon to actually manage the registry
and Traefik containers on startup. Tasks 13-14 build the CLI. Task 15 is
the only task that touches a real Docker daemon end-to-end.

---

### Task 1: Project scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/cubeshipd/main.go`
- Create: `cmd/cubeship/main.go`
- Test: `cmd/cubeshipd/main_test.go`

**Interfaces:**
- Produces: two buildable binaries, `cubeshipd` and `cubeship`, each
  printing a version string on `--version`.

- [ ] **Step 1: Init the module**

```bash
cd /Users/lucas/Developer/Cubeship
go mod init cubeship
```

- [ ] **Step 2: Write the failing test for the daemon entrypoint**

`cmd/cubeshipd/main_test.go`:
```go
package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	out, err := exec.Command("go", "run", ".", "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "cubeshipd") {
		t.Fatalf("expected version output to contain cubeshipd, got: %s", out)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./cmd/cubeshipd/...`
Expected: FAIL (no `main.go` yet / build error)

- [ ] **Step 3: Write the minimal daemon entrypoint**

`cmd/cubeshipd/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeshipd %s\n", version)
		os.Exit(0)
	}
	fmt.Println("cubeshipd: no command implemented yet")
}
```

- [ ] **Step 4: Run test to confirm it passes**

Run: `go test ./cmd/cubeshipd/...`
Expected: PASS

- [ ] **Step 5: Write the CLI entrypoint (no test — trivial stub)**

`cmd/cubeship/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeship %s\n", version)
		os.Exit(0)
	}
	fmt.Println("cubeship: no command implemented yet")
}
```

- [ ] **Step 6: Commit**

```bash
git add go.mod cmd
git commit -m "Scaffold cubeshipd and cubeship binaries"
```

---

### Task 2: SQLite store — apps

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/apps.go`
- Test: `internal/store/apps_test.go`
- Modify: `go.mod` (add `modernc.org/sqlite`)

**Interfaces:**
- Produces:
  - `store.Open(path string) (*store.Store, error)`
  - `type store.App struct { ID int64; Name string; Domain string; Image string; ContainerID string; Status string; CreatedAt time.Time }`
  - `(*Store).CreateApp(ctx, name, domain, image string) (*App, error)`
  - `(*Store).GetAppByName(ctx, name string) (*App, error)`
  - `(*Store).GetAppByImage(ctx, imageRepo string) (*App, error)`
  - `(*Store).ListApps(ctx) ([]*App, error)`
  - `(*Store).UpdateAppContainer(ctx, appID int64, containerID, status string) error`

- [ ] **Step 1: Add the SQLite dependency**

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Write the failing test**

`internal/store/apps_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCreateAndGetApp(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	created, err := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Domain != "myapp.example.com" || got.Image != "registry.example.com/myapp" {
		t.Fatalf("unexpected app: %+v", got)
	}
	if got.Status != "pending" {
		t.Fatalf("expected initial status 'pending', got %q", got.Status)
	}
}

func TestGetAppByImage(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	got, err := s.GetAppByImage(ctx, "registry.example.com/myapp")
	if err != nil {
		t.Fatalf("GetAppByImage: %v", err)
	}
	if got.Name != "myapp" {
		t.Fatalf("expected myapp, got %q", got.Name)
	}

	_, err = s.GetAppByImage(ctx, "registry.example.com/unknown")
	if err == nil {
		t.Fatal("expected error for unknown image")
	}
}

func TestUpdateAppContainer(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	created, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := s.UpdateAppContainer(ctx, created.ID, "abc123", "running"); err != nil {
		t.Fatalf("UpdateAppContainer: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "abc123" || got.Status != "running" {
		t.Fatalf("unexpected app after update: %+v", got)
	}
}
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/store/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 4: Write the store**

`internal/store/store.go`:
```go
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS deployments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id INTEGER NOT NULL REFERENCES apps(id),
	image_ref TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
```

`internal/store/apps.go`:
```go
package store

import (
	"context"
	"fmt"
	"time"
)

type App struct {
	ID          int64
	Name        string
	Domain      string
	Image       string
	ContainerID string
	Status      string
	CreatedAt   time.Time
}

func (s *Store) CreateApp(ctx context.Context, name, domain, image string) (*App, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO apps (name, domain, image) VALUES (?, ?, ?)`,
		name, domain, image)
	if err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetAppByName(ctx, name)
	_ = id
}

func (s *Store) scanApp(row interface {
	Scan(dest ...any) error
}) (*App, error) {
	var a App
	if err := row.Scan(&a.ID, &a.Name, &a.Domain, &a.Image, &a.ContainerID, &a.Status, &a.CreatedAt); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) GetAppByName(ctx context.Context, name string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, domain, image, container_id, status, created_at FROM apps WHERE name = ?`, name)
	a, err := s.scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app %q: %w", name, err)
	}
	return a, nil
}

func (s *Store) GetAppByImage(ctx context.Context, image string) (*App, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, domain, image, container_id, status, created_at FROM apps WHERE image = ?`, image)
	a, err := s.scanApp(row)
	if err != nil {
		return nil, fmt.Errorf("get app by image %q: %w", image, err)
	}
	return a, nil
}

func (s *Store) ListApps(ctx context.Context) ([]*App, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, domain, image, container_id, status, created_at FROM apps ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []*App
	for rows.Next() {
		a, err := s.scanApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (s *Store) UpdateAppContainer(ctx context.Context, appID int64, containerID, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE apps SET container_id = ?, status = ? WHERE id = ?`,
		containerID, status, appID)
	if err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	return nil
}
```

Note: `CreateApp` has a dead `_ = id` line after an early `return` — fix
it while typing this in: drop the `id` variable entirely since
`GetAppByName` is what actually returns the row.

- [ ] **Step 5: Fix the dead code from Step 4**

Replace the body of `CreateApp` in `internal/store/apps.go`:
```go
func (s *Store) CreateApp(ctx context.Context, name, domain, image string) (*App, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO apps (name, domain, image) VALUES (?, ?, ?)`,
		name, domain, image); err != nil {
		return nil, fmt.Errorf("create app: %w", err)
	}
	return s.GetAppByName(ctx, name)
}
```

- [ ] **Step 6: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/store
git commit -m "Add SQLite-backed app store"
```

---

### Task 3: SQLite store — deployment history

**Files:**
- Create: `internal/store/deployments.go`
- Test: `internal/store/deployments_test.go`

**Interfaces:**
- Consumes: `store.Store` from Task 2.
- Produces:
  - `type store.Deployment struct { ID int64; AppID int64; ImageRef string; Status string; Error string; CreatedAt time.Time }`
  - `(*Store).RecordDeployment(ctx, appID int64, imageRef, status, errMsg string) error`
  - `(*Store).ListDeployments(ctx, appID int64) ([]*Deployment, error)`

- [ ] **Step 1: Write the failing test**

`internal/store/deployments_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestRecordAndListDeployments(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := s.RecordDeployment(ctx, app.ID, "registry.example.com/myapp:latest", "success", ""); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}
	if err := s.RecordDeployment(ctx, app.ID, "registry.example.com/myapp:v2", "failed", "health check timeout"); err != nil {
		t.Fatalf("RecordDeployment: %v", err)
	}

	deps, err := s.ListDeployments(ctx, app.ID)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 deployments, got %d", len(deps))
	}
	if deps[1].Status != "failed" || deps[1].Error != "health check timeout" {
		t.Fatalf("unexpected second deployment: %+v", deps[1])
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/store/... -run TestRecordAndListDeployments`
Expected: FAIL (`RecordDeployment` undefined)

- [ ] **Step 3: Implement**

`internal/store/deployments.go`:
```go
package store

import (
	"context"
	"fmt"
	"time"
)

type Deployment struct {
	ID        int64
	AppID     int64
	ImageRef  string
	Status    string
	Error     string
	CreatedAt time.Time
}

func (s *Store) RecordDeployment(ctx context.Context, appID int64, imageRef, status, errMsg string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, image_ref, status, error) VALUES (?, ?, ?, ?)`,
		appID, imageRef, status, errMsg)
	if err != nil {
		return fmt.Errorf("record deployment: %w", err)
	}
	return nil
}

func (s *Store) ListDeployments(ctx context.Context, appID int64) ([]*Deployment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, app_id, image_ref, status, error, created_at FROM deployments WHERE app_id = ? ORDER BY id`,
		appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []*Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.AppID, &d.ImageRef, &d.Status, &d.Error, &d.CreatedAt); err != nil {
			return nil, err
		}
		deps = append(deps, &d)
	}
	return deps, rows.Err()
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS (all store tests)

- [ ] **Step 5: Commit**

```bash
git add internal/store
git commit -m "Add deployment history to the store"
```

---

### Task 4: Docker client wrapper

**Files:**
- Create: `internal/dockerx/client.go`
- Create: `internal/dockerx/containers.go`
- Test: `internal/dockerx/containers_test.go`
- Modify: `go.mod` (add `github.com/docker/docker`, `github.com/opencontainers/image-spec`)

**Interfaces:**
- Produces:
  - `type dockerx.ContainerOpts struct { Name string; Image string; Labels map[string]string; Env []string }`
  - `dockerx.New() (*dockerx.Client, error)` — real client, talks to the local Docker socket
  - `(*Client).PullImage(ctx, ref string) error`
  - `(*Client).CreateContainer(ctx, opts ContainerOpts) (id string, err error)`
  - `(*Client).StartContainer(ctx, id string) error`
  - `(*Client).StopContainer(ctx, id string) error`
  - `(*Client).RemoveContainer(ctx, id string) error`
  - `(*Client).IsRunning(ctx, id string) (bool, error)`

The wrapper depends on a narrow, package-private `apiClient` interface
(only the Docker SDK methods it actually calls), not the concrete
`*client.Client`. This lets tests substitute a fake without a real
Docker daemon; `*client.Client` satisfies the interface structurally.

- [ ] **Step 1: Add the Docker SDK dependency**

```bash
go get github.com/docker/docker@v25.0.6+incompatible
go get github.com/opencontainers/image-spec@v1.1.0
```

- [ ] **Step 2: Write the failing test**

`internal/dockerx/containers_test.go`:
```go
package dockerx

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeAPI struct {
	pulledRef       string
	createdConfig   *container.Config
	createdName     string
	startedID       string
	stoppedID       string
	removedID       string
	inspectedRunning bool
	inspectErr      error
}

func (f *fakeAPI) ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error) {
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
```

- [ ] **Step 3: Run to confirm it fails**

Run: `go test ./internal/dockerx/...`
Expected: FAIL (package doesn't exist yet)

- [ ] **Step 4: Implement the client wrapper**

`internal/dockerx/client.go`:
```go
package dockerx

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// apiClient is the subset of the Docker Engine API this package uses.
// *dockerclient.Client satisfies it structurally.
//
// NOTE: these method signatures match github.com/docker/docker v25.0.6.
// If `go build` fails after `go get` pulled a different version, adjust
// the option struct types to whatever the compiler error names — the
// compiler is the source of truth for the installed SDK version.
type apiClient interface {
	ImagePull(ctx context.Context, ref string, options image.PullOptions) (io.ReadCloser, error)
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
}

type Client struct {
	api apiClient
}

func New() (*Client, error) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{api: cli}, nil
}

// newWithAPI is used by tests to inject a fake apiClient.
func newWithAPI(api apiClient) *Client {
	return &Client{api: api}
}
```

`internal/dockerx/containers.go`:
```go
package dockerx

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
)

type ContainerOpts struct {
	Name   string
	Image  string
	Labels map[string]string
	Env    []string
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	rc, err := c.api.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer rc.Close()
	// Drain the pull progress stream; callers don't need the output.
	buf := make([]byte, 4096)
	for {
		if _, err := rc.Read(buf); err != nil {
			break
		}
	}
	return nil
}

func (c *Client) CreateContainer(ctx context.Context, opts ContainerOpts) (string, error) {
	resp, err := c.api.ContainerCreate(ctx,
		&container.Config{
			Image:  opts.Image,
			Labels: opts.Labels,
			Env:    opts.Env,
		},
		&container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
		},
		nil, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("create container %q: %w", opts.Name, err)
	}
	return resp.ID, nil
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %q: %w", id, err)
	}
	return nil
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
		return fmt.Errorf("stop container %q: %w", id, err)
	}
	return nil
}

func (c *Client) RemoveContainer(ctx context.Context, id string) error {
	if err := c.api.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove container %q: %w", id, err)
	}
	return nil
}

func (c *Client) IsRunning(ctx context.Context, id string) (bool, error) {
	info, err := c.api.ContainerInspect(ctx, id)
	if err != nil {
		return false, fmt.Errorf("inspect container %q: %w", id, err)
	}
	return info.State != nil && info.State.Running, nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/dockerx/...`
Expected: PASS. If it fails to compile, the installed Docker SDK
version has different option-struct field names than assumed above —
follow the compiler error to the actual field names and fix `client.go`
and `containers.go` accordingly, then re-run.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/dockerx
git commit -m "Add Docker client wrapper"
```

---

### Task 5: Traefik label builder

**Files:**
- Create: `internal/traefik/labels.go`
- Test: `internal/traefik/labels_test.go`

**Interfaces:**
- Produces: `traefik.Labels(appName, domain string, port int) map[string]string`

- [ ] **Step 1: Write the failing test**

`internal/traefik/labels_test.go`:
```go
package traefik

import "testing"

func TestLabels(t *testing.T) {
	labels := Labels("myapp", "myapp.example.com", 8080)

	want := map[string]string{
		"traefik.enable":                                              "true",
		"traefik.http.routers.cubeship-myapp.rule":                    "Host(`myapp.example.com`)",
		"traefik.http.routers.cubeship-myapp.entrypoints":              "websecure",
		"traefik.http.routers.cubeship-myapp.tls.certresolver":         "letsencrypt",
		"traefik.http.services.cubeship-myapp.loadbalancer.server.port": "8080",
		"traefik.docker.network":                                       "cubeship",
	}

	for k, v := range want {
		if labels[k] != v {
			t.Errorf("label %q: got %q, want %q", k, labels[k], v)
		}
	}
	if len(labels) != len(want) {
		t.Errorf("got %d labels, want %d: %v", len(labels), len(want), labels)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/traefik/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/traefik/labels.go`:
```go
package traefik

import (
	"fmt"
	"strconv"
)

// Labels returns the Docker labels that make Traefik route the given
// domain to an app container's port, with automatic TLS.
func Labels(appName, domain string, port int) map[string]string {
	router := "cubeship-" + appName
	return map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers." + router + ".rule":            fmt.Sprintf("Host(`%s`)", domain),
		"traefik.http.routers." + router + ".entrypoints":      "websecure",
		"traefik.http.routers." + router + ".tls.certresolver": "letsencrypt",
		"traefik.http.services." + router + ".loadbalancer.server.port": strconv.Itoa(port),
		"traefik.docker.network": "cubeship",
	}
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/traefik/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/traefik
git commit -m "Add Traefik label builder"
```

---

### Task 6: Deploy orchestrator

**Files:**
- Create: `internal/deploy/orchestrator.go`
- Test: `internal/deploy/orchestrator_test.go`

**Interfaces:**
- Consumes:
  - `*store.Store` and its methods from Tasks 2-3.
  - `dockerx.ContainerOpts` from Task 4 (structurally-typed against a
    package-private `dockerAPI` interface here, satisfied by
    `*dockerx.Client`).
  - `traefik.Labels(appName, domain string, port int) map[string]string` from Task 5.
- Produces:
  - `deploy.New(s *store.Store, d dockerAPI) *Orchestrator` — package-private constructor signature; exported constructor is `deploy.NewOrchestrator(s *store.Store, d *dockerx.Client) *Orchestrator`
  - `(*Orchestrator).Deploy(ctx context.Context, appName, imageRef string) error`
  - `Orchestrator.HealthCheckAttempts int`, `Orchestrator.HealthCheckInterval time.Duration` (exported fields, overridable in tests)

**Convention (document, not enforce yet):** app containers are expected
to listen on port 8080. A per-app configurable port is out of scope for
this sub-project.

- [ ] **Step 1: Write the failing test**

`internal/deploy/orchestrator_test.go`:
```go
package deploy

import (
	"context"
	"errors"
	"testing"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type fakeDocker struct {
	nextCreateID   string
	running        bool
	pulledRef      string
	createdOpts    dockerx.ContainerOpts
	startedID      string
	stoppedIDs     []string
	removedIDs     []string
	createErr      error
	startErr       error
}

func (f *fakeDocker) PullImage(ctx context.Context, ref string) error {
	f.pulledRef = ref
	return nil
}

func (f *fakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createdOpts = opts
	return f.nextCreateID, nil
}

func (f *fakeDocker) StartContainer(ctx context.Context, id string) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.startedID = id
	return nil
}

func (f *fakeDocker) StopContainer(ctx context.Context, id string) error {
	f.stoppedIDs = append(f.stoppedIDs, id)
	return nil
}

func (f *fakeDocker) RemoveContainer(ctx context.Context, id string) error {
	f.removedIDs = append(f.removedIDs, id)
	return nil
}

func (f *fakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return f.running, nil
}

func newTestOrchestrator(t *testing.T, docker *fakeDocker) (*Orchestrator, *store.Store) {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	o := New(s, docker)
	o.HealthCheckAttempts = 1
	o.HealthCheckInterval = 0
	return o, s
}

func TestDeploySuccessFirstDeploy(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if docker.pulledRef != "registry.example.com/myapp:latest" {
		t.Fatalf("expected pull of the new ref, got %q", docker.pulledRef)
	}
	if docker.createdOpts.Labels["traefik.enable"] != "true" {
		t.Fatalf("expected traefik labels to be set, got %v", docker.createdOpts.Labels)
	}
	if docker.startedID != "container-1" {
		t.Fatalf("expected the new container to be started, got %q", docker.startedID)
	}
	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected no container stopped on first deploy, got %v", docker.stoppedIDs)
	}

	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" || app.Status != "running" {
		t.Fatalf("unexpected app state after deploy: %+v", app)
	}

	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "success" {
		t.Fatalf("expected one successful deployment record, got %+v", deps)
	}
}

func TestDeploySwapsOldContainer(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: true}
	o, s := newTestOrchestrator(t, docker)

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:v2"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(docker.stoppedIDs) != 1 || docker.stoppedIDs[0] != "container-1" {
		t.Fatalf("expected old container-1 to be stopped, got %v", docker.stoppedIDs)
	}
	if len(docker.removedIDs) != 1 || docker.removedIDs[0] != "container-1" {
		t.Fatalf("expected old container-1 to be removed, got %v", docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-2" {
		t.Fatalf("expected app to point at the new container, got %q", got.ContainerID)
	}
}

func TestDeployHealthCheckFailureLeavesOldContainerRunning(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-2", running: false}
	o, s := newTestOrchestrator(t, docker)

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:v2")
	if err == nil {
		t.Fatal("expected Deploy to return an error on failed health check")
	}

	if len(docker.stoppedIDs) != 0 {
		t.Fatalf("expected the healthy old container to stay untouched, got stopped: %v", docker.stoppedIDs)
	}
	if len(docker.removedIDs) != 1 || docker.removedIDs[0] != "container-2" {
		t.Fatalf("expected the failed new container to be removed, got %v", docker.removedIDs)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.ContainerID != "container-1" {
		t.Fatalf("expected app to still point at the old container, got %q", got.ContainerID)
	}

	deps, _ := s.ListDeployments(ctx, app.ID)
	if len(deps) != 1 || deps[0].Status != "failed" {
		t.Fatalf("expected one failed deployment record, got %+v", deps)
	}
}

func TestDeployUnknownApp(t *testing.T) {
	ctx := context.Background()
	o, _ := newTestOrchestrator(t, &fakeDocker{})

	err := o.Deploy(ctx, "does-not-exist", "registry.example.com/does-not-exist:latest")
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("expected ErrAppNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/deploy/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/deploy/orchestrator.go`:
```go
package deploy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cubeship/internal/dockerx"
	"cubeship/internal/store"
	"cubeship/internal/traefik"
)

// appPort is the port every app container is expected to listen on.
// A per-app configurable port is out of scope for this sub-project.
const appPort = 8080

var ErrAppNotFound = errors.New("app not found")

// dockerAPI is the subset of dockerx.Client this package needs.
// *dockerx.Client satisfies it structurally.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
	StopContainer(ctx context.Context, id string) error
	RemoveContainer(ctx context.Context, id string) error
	IsRunning(ctx context.Context, id string) (bool, error)
}

type Orchestrator struct {
	store  *store.Store
	docker dockerAPI

	HealthCheckAttempts int
	HealthCheckInterval time.Duration
}

func New(s *store.Store, d dockerAPI) *Orchestrator {
	return &Orchestrator{
		store:               s,
		docker:              d,
		HealthCheckAttempts: 10,
		HealthCheckInterval: 500 * time.Millisecond,
	}
}

func NewOrchestrator(s *store.Store, d *dockerx.Client) *Orchestrator {
	return New(s, d)
}

func (o *Orchestrator) Deploy(ctx context.Context, appName, imageRef string) error {
	app, err := o.store.GetAppByName(ctx, appName)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}

	if err := o.docker.PullImage(ctx, imageRef); err != nil {
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("pull image: %w", err)
	}

	newName := fmt.Sprintf("cubeship-%s-%d", appName, time.Now().UnixNano())
	labels := traefik.Labels(appName, app.Domain, appPort)

	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:   newName,
		Image:  imageRef,
		Labels: labels,
	})
	if err != nil {
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("create container: %w", err)
	}

	if err := o.docker.StartContainer(ctx, newID); err != nil {
		o.docker.RemoveContainer(ctx, newID)
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", err.Error())
		return fmt.Errorf("start container: %w", err)
	}

	if !o.waitHealthy(ctx, newID) {
		o.docker.RemoveContainer(ctx, newID)
		o.store.RecordDeployment(ctx, app.ID, imageRef, "failed", "health check timed out")
		return fmt.Errorf("health check timed out for container %s", newID)
	}

	oldContainerID := app.ContainerID
	if err := o.store.UpdateAppContainer(ctx, app.ID, newID, "running"); err != nil {
		return fmt.Errorf("update app container: %w", err)
	}
	o.store.RecordDeployment(ctx, app.ID, imageRef, "success", "")

	if oldContainerID != "" {
		o.docker.StopContainer(ctx, oldContainerID)
		o.docker.RemoveContainer(ctx, oldContainerID)
	}

	return nil
}

func (o *Orchestrator) waitHealthy(ctx context.Context, containerID string) bool {
	for i := 0; i < o.HealthCheckAttempts; i++ {
		running, err := o.docker.IsRunning(ctx, containerID)
		if err == nil && running {
			return true
		}
		if o.HealthCheckInterval > 0 {
			time.Sleep(o.HealthCheckInterval)
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/deploy/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/deploy
git commit -m "Add zero-downtime deploy orchestrator"
```

---

### Task 7: Daemon HTTP server skeleton + auth middleware

**Files:**
- Create: `internal/api/server.go`
- Test: `internal/api/server_test.go`

**Interfaces:**
- Produces:
  - `api.NewServer(s *store.Store, orch *deploy.Orchestrator, token, registryHost string) *Server`
  - `(*Server).Router() http.Handler`
  - `(*Server).mux *http.ServeMux` (unexported field later handlers register onto — Task 8+ add routes by extending `Router()`, not by re-declaring the mux)

- [ ] **Step 1: Write the failing test**

`internal/api/server_test.go`:
```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzIsUnauthenticated(t *testing.T) {
	s := NewServer(nil, nil, "secret-token", "registry.example.com")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	s := NewServer(nil, nil, "secret-token", "registry.example.com")
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsCorrectToken(t *testing.T) {
	s := NewServer(nil, nil, "secret-token", "registry.example.com")
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsWrongToken(t *testing.T) {
	s := NewServer(nil, nil, "secret-token", "registry.example.com")
	protected := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/api/server.go`:
```go
package api

import (
	"net/http"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

type Server struct {
	store        *store.Store
	orch         *deploy.Orchestrator
	token        string
	registryHost string
	mux          *http.ServeMux
}

func NewServer(s *store.Store, orch *deploy.Orchestrator, token, registryHost string) *Server {
	srv := &Server{
		store:        s,
		orch:         orch,
		token:        token,
		registryHost: registryHost,
		mux:          http.NewServeMux(),
	}
	srv.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return srv
}

func (s *Server) Router() http.Handler {
	return s.mux
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuth registers a handler on the mux behind authMiddleware.
// Task 8+ use this instead of calling s.mux.HandleFunc directly.
func (s *Server) handleAuth(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.authMiddleware(h))
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api
git commit -m "Add daemon HTTP server skeleton with token auth"
```

---

### Task 8: API — app CRUD handlers

**Files:**
- Create: `internal/api/apps_handlers.go`
- Test: `internal/api/apps_handlers_test.go`
- Modify: `internal/api/server.go` (register routes in `NewServer`)

**Interfaces:**
- Consumes: `store.Store` (Task 2), `Server.handleAuth` (Task 7).
- Produces (HTTP contract):
  - `POST /apps` — body `{"name":"...","domain":"..."}` → 201 with
    `{"name":"...","domain":"...","image":"..."}`; 400 on missing
    fields; 409 if the name already exists.
  - `GET /apps` — 200 with a JSON array of the same shape plus
    `"status"`.
  - `GET /apps/{name}` — 200 with one app; 404 if not found.

- [ ] **Step 1: Write the failing test**

`internal/api/apps_handlers_test.go`:
```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewServer(s, nil, "secret-token", "registry.example.com")
}

func authedRequest(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestCreateAppReturnsImagePath(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})
	req := authedRequest(http.MethodPost, "/apps", body)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["image"] != "registry.example.com/myapp" {
		t.Fatalf("expected image registry.example.com/myapp, got %q", got["image"])
	}
}

func TestCreateAppMissingFields(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp"})
	req := authedRequest(http.MethodPost, "/apps", body)
	rec := httptest.NewRecorder()

	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateAppDuplicateName(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})

	rec1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec1, authedRequest(http.MethodPost, "/apps", body))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec2, authedRequest(http.MethodPost, "/apps", body))
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d", rec2.Code)
	}
}

func TestListAndGetApp(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"name": "myapp", "domain": "myapp.example.com"})
	srv.Router().ServeHTTP(httptest.NewRecorder(), authedRequest(http.MethodPost, "/apps", body))

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, authedRequest(http.MethodGet, "/apps", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", listRec.Code)
	}
	var apps []map[string]any
	json.Unmarshal(listRec.Body.Bytes(), &apps)
	if len(apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(apps))
	}

	getRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(getRec, authedRequest(http.MethodGet, "/apps/myapp", nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	missRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(missRec, authedRequest(http.MethodGet, "/apps/nope", nil))
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missRec.Code)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run 'TestCreateApp|TestListAndGetApp'`
Expected: FAIL (routes not registered / undefined)

- [ ] **Step 3: Implement**

`internal/api/apps_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"cubeship/internal/store"
)

type appResponse struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

func toAppResponse(a *store.App) appResponse {
	return appResponse{Name: a.Name, Domain: a.Domain, Image: a.Image, Status: a.Status}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleCreateApp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Domain == "" {
		http.Error(w, "name and domain are required", http.StatusBadRequest)
		return
	}

	if _, err := s.store.GetAppByName(r.Context(), req.Name); err == nil {
		http.Error(w, "app already exists", http.StatusConflict)
		return
	}

	image := s.registryHost + "/" + req.Name
	app, err := s.store.CreateApp(r.Context(), req.Name, req.Domain, image)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, toAppResponse(app))
}

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps, err := s.store.ListApps(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		resp = append(resp, toAppResponse(a))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	app, err := s.store.GetAppByName(r.Context(), name)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, toAppResponse(app))
}
```

- [ ] **Step 4: Register the routes**

In `internal/api/server.go`, inside `NewServer`, after the `/healthz`
registration:
```go
	srv.handleAuth("POST /apps", srv.handleCreateApp)
	srv.handleAuth("GET /apps", srv.handleListApps)
	srv.handleAuth("GET /apps/{name}", srv.handleGetApp)
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "Add app CRUD API handlers"
```

---

### Task 9: API — registry push webhook

**Files:**
- Create: `internal/api/webhook_handler.go`
- Test: `internal/api/webhook_handler_test.go`
- Modify: `internal/api/server.go` (register the route, unauthenticated
  — see note below)

**Interfaces:**
- Consumes: `deploy.Orchestrator.Deploy` (Task 6), `store.GetAppByImage` (Task 2).
- Produces (HTTP contract): `POST /hooks/registry` accepts the Docker
  Registry v2 notification payload
  (`{"events":[{"action":"push","target":{"repository":"...","tag":"..."}}]}`).
  For each `push` event whose repository matches a tracked app's image,
  it synchronously calls `Deploy`. Always responds `200` (the registry
  should not retry just because an unmatched or already-processed event
  arrived). Unknown repositories are silently ignored, per spec.

**Design note:** this endpoint is intentionally NOT behind
`authMiddleware` — the registry container has no bearer token to send.
It must only be reachable from inside the Docker network the registry
and daemon share, never exposed publicly; Task 12 wires that network
placement.

- [ ] **Step 1: Write the failing test**

`internal/api/webhook_handler_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/store"
)

type webhookFakeDocker struct {
	running      bool
	pulledRef    string
	createCalled bool
}

func (f *webhookFakeDocker) PullImage(ctx context.Context, ref string) error {
	f.pulledRef = ref
	return nil
}
func (f *webhookFakeDocker) CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error) {
	f.createCalled = true
	return "container-1", nil
}
func (f *webhookFakeDocker) StartContainer(ctx context.Context, id string) error { return nil }
func (f *webhookFakeDocker) StopContainer(ctx context.Context, id string) error  { return nil }
func (f *webhookFakeDocker) RemoveContainer(ctx context.Context, id string) error { return nil }
func (f *webhookFakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	return f.running, nil
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

func TestRegistryWebhookTriggersDeployForMatchedApp(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 1
	orch.HealthCheckInterval = 0

	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(registryNotificationPayload)))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if docker.pulledRef != "registry.example.com/myapp:latest" {
		t.Fatalf("expected deploy to pull registry.example.com/myapp:latest, got %q", docker.pulledRef)
	}

	app, _ := s.GetAppByName(ctx, "myapp")
	if app.ContainerID != "container-1" {
		t.Fatalf("expected app to be deployed, got container ID %q", app.ContainerID)
	}
}

func TestRegistryWebhookIgnoresUnknownRepository(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(`{
		"events": [{"action": "push", "target": {"repository": "unknown-app", "tag": "latest"}}]
	}`)))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even for unmatched repository, got %d", rec.Code)
	}
	if docker.createCalled {
		t.Fatal("expected no container to be created for an unknown repository")
	}
}

func TestRegistryWebhookRequiresNoAuth(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	orch := deploy.New(s, &webhookFakeDocker{})
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := httptest.NewRequest(http.MethodPost, "/hooks/registry", bytes.NewReader([]byte(`{"events":[]}`)))
	// deliberately no Authorization header
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 without auth, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/api/... -run TestRegistryWebhook`
Expected: FAIL (route not registered)

- [ ] **Step 3: Implement**

`internal/api/webhook_handler.go`:
```go
package api

import (
	"encoding/json"
	"log"
	"net/http"
)

type registryNotification struct {
	Events []struct {
		Action string `json:"action"`
		Target struct {
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"target"`
	} `json:"events"`
}

func (s *Server) handleRegistryWebhook(w http.ResponseWriter, r *http.Request) {
	var payload registryNotification
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		// Malformed payload from a source we don't control: log and
		// still 200, there is nothing a retry would fix.
		log.Printf("registry webhook: invalid payload: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	for _, ev := range payload.Events {
		if ev.Action != "push" || ev.Target.Tag == "" {
			continue
		}
		image := s.registryHost + "/" + ev.Target.Repository
		app, err := s.store.GetAppByImage(r.Context(), image)
		if err != nil {
			continue // no app tracks this repository
		}
		imageRef := image + ":" + ev.Target.Tag
		if err := s.orch.Deploy(r.Context(), app.Name, imageRef); err != nil {
			log.Printf("registry webhook: deploy failed for %s: %v", app.Name, err)
		}
	}
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 4: Register the route (unauthenticated)**

In `internal/api/server.go`, inside `NewServer`, alongside the
`/healthz` registration (not via `handleAuth`):
```go
	srv.mux.HandleFunc("POST /hooks/registry", srv.handleRegistryWebhook)
```

- [ ] **Step 5: Run tests to confirm they pass**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "Add registry push webhook that triggers deploys"
```

---

### Task 10: Env vars + manual deploy endpoint

**Files:**
- Modify: `internal/store/store.go` (schema: add `env` column)
- Modify: `internal/store/apps.go` (`App.Env` field, `scanApp`, new `SetAppEnv`)
- Test: `internal/store/apps_test.go` (add env test)
- Modify: `internal/deploy/orchestrator.go` (`Deploy` passes `app.Env` to the container)
- Test: `internal/deploy/orchestrator_test.go` (add env-forwarding test)
- Create: `internal/api/deploy_handlers.go`
- Test: `internal/api/deploy_handlers_test.go`
- Modify: `internal/api/server.go` (register the two routes)

**Interfaces:**
- Produces:
  - `store.App.Env map[string]string` (new field)
  - `(*Store).SetAppEnv(ctx, appID int64, env map[string]string) error`
  - `POST /apps/{name}/deploy` — body `{"tag":"latest"}` (tag optional,
    defaults to `"latest"`) → 200 on success, 502 with `{"error":"..."}`
    on deploy failure.
  - `PUT /apps/{name}/env` — body `{"vars":{"KEY":"VALUE"}}` → replaces
    the app's env vars, 200. Does not itself trigger a redeploy (the
    next push or manual deploy picks it up) — this matches the spec's
    "push is the trigger" model without a surprise side effect on a
    plain config write.

- [ ] **Step 1: Write the failing store test for env**

Add to `internal/store/apps_test.go`:
```go
func TestSetAndGetAppEnv(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	ctx := context.Background()
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	if len(app.Env) != 0 {
		t.Fatalf("expected empty env on creation, got %v", app.Env)
	}

	if err := s.SetAppEnv(ctx, app.ID, map[string]string{"PORT": "8080", "LOG_LEVEL": "info"}); err != nil {
		t.Fatalf("SetAppEnv: %v", err)
	}

	got, err := s.GetAppByName(ctx, "myapp")
	if err != nil {
		t.Fatalf("GetAppByName: %v", err)
	}
	if got.Env["PORT"] != "8080" || got.Env["LOG_LEVEL"] != "info" {
		t.Fatalf("unexpected env: %v", got.Env)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/store/... -run TestSetAndGetAppEnv`
Expected: FAIL (`SetAppEnv` undefined, `Env` field missing)

- [ ] **Step 3: Add the schema column**

In `internal/store/store.go`, change the `apps` table definition inside
`schema`:
```go
CREATE TABLE IF NOT EXISTS apps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	domain TEXT NOT NULL,
	image TEXT NOT NULL UNIQUE,
	container_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	env TEXT NOT NULL DEFAULT '{}',
	created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 4: Add the `Env` field, update scanning, add `SetAppEnv`**

In `internal/store/apps.go`, update the `App` struct and `scanApp`, and
add `SetAppEnv`:
```go
type App struct {
	ID          int64
	Name        string
	Domain      string
	Image       string
	ContainerID string
	Status      string
	Env         map[string]string
	CreatedAt   time.Time
}

func (s *Store) scanApp(row interface {
	Scan(dest ...any) error
}) (*App, error) {
	var a App
	var envJSON string
	if err := row.Scan(&a.ID, &a.Name, &a.Domain, &a.Image, &a.ContainerID, &a.Status, &envJSON, &a.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(envJSON), &a.Env); err != nil {
		return nil, fmt.Errorf("decode env for app %q: %w", a.Name, err)
	}
	return &a, nil
}

func (s *Store) SetAppEnv(ctx context.Context, appID int64, env map[string]string) error {
	envJSON, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode env: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE apps SET env = ? WHERE id = ?`, string(envJSON), appID); err != nil {
		return fmt.Errorf("set app env: %w", err)
	}
	return nil
}
```

Also update every `SELECT` in `apps.go` (`GetAppByName`, `GetAppByImage`,
`ListApps`) to select the new `env` column in the same position used
above: `id, name, domain, image, container_id, status, env, created_at`.
Add `"encoding/json"` to the imports.

- [ ] **Step 5: Run store tests to confirm they pass**

Run: `go test ./internal/store/...`
Expected: PASS

- [ ] **Step 6: Write the failing orchestrator test for env forwarding**

Add to `internal/deploy/orchestrator_test.go`:
```go
func TestDeployForwardsAppEnv(t *testing.T) {
	ctx := context.Background()
	docker := &fakeDocker{nextCreateID: "container-1", running: true}
	o, s := newTestOrchestrator(t, docker)

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.SetAppEnv(ctx, app.ID, map[string]string{"PORT": "8080"})

	if err := o.Deploy(ctx, "myapp", "registry.example.com/myapp:latest"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	found := false
	for _, kv := range docker.createdOpts.Env {
		if kv == "PORT=8080" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected PORT=8080 in container env, got %v", docker.createdOpts.Env)
	}
}
```

- [ ] **Step 7: Run to confirm it fails**

Run: `go test ./internal/deploy/... -run TestDeployForwardsAppEnv`
Expected: FAIL (env not forwarded, slice empty)

- [ ] **Step 8: Forward env vars in `Deploy`**

In `internal/deploy/orchestrator.go`, add the import `"sort"` and change
the `CreateContainer` call:
```go
	newID, err := o.docker.CreateContainer(ctx, dockerx.ContainerOpts{
		Name:   newName,
		Image:  imageRef,
		Labels: labels,
		Env:    envSlice(app.Env),
	})
```

Add the helper below `waitHealthy`:
```go
func envSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}
```

- [ ] **Step 9: Run deploy tests to confirm they pass**

Run: `go test ./internal/deploy/...`
Expected: PASS

- [ ] **Step 10: Write the failing API test**

`internal/api/deploy_handlers_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestManualDeployEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 1
	orch.HealthCheckInterval = 0
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	body, _ := json.Marshal(map[string]string{"tag": "v2"})
	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", body)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if docker.pulledRef != "registry.example.com/myapp:v2" {
		t.Fatalf("expected pull of tag v2, got %q", docker.pulledRef)
	}
}

func TestManualDeployDefaultsToLatestTag(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	docker := &webhookFakeDocker{running: true}
	orch := deploy.New(s, docker)
	orch.HealthCheckAttempts = 1
	srv := NewServer(s, orch, "secret-token", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/myapp/deploy", []byte(`{}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if docker.pulledRef != "registry.example.com/myapp:latest" {
		t.Fatalf("expected default tag latest, got %q", docker.pulledRef)
	}
}

func TestSetEnvEndpoint(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "secret-token", "registry.example.com")

	body, _ := json.Marshal(map[string]map[string]string{"vars": {"PORT": "9090"}})
	req := authedRequest(http.MethodPut, "/apps/myapp/env", body)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Env["PORT"] != "9090" {
		t.Fatalf("expected env to be persisted, got %v", got.Env)
	}
}

func TestManualDeployUnknownApp(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodPost, "/apps/nope/deploy", []byte(`{}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

- [ ] **Step 11: Run to confirm it fails**

Run: `go test ./internal/api/... -run 'TestManualDeploy|TestSetEnv'`
Expected: FAIL (routes not registered / handlers undefined)

- [ ] **Step 12: Implement the handlers**

`internal/api/deploy_handlers.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleManualDeploy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	app, err := s.store.GetAppByName(r.Context(), name)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Tag string `json:"tag"`
	}
	json.NewDecoder(r.Body).Decode(&req) // empty/absent body is fine, Tag stays ""
	if req.Tag == "" {
		req.Tag = "latest"
	}

	imageRef := app.Image + ":" + req.Tag
	if err := s.orch.Deploy(r.Context(), app.Name, imageRef); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSetEnv(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	app, err := s.store.GetAppByName(r.Context(), name)
	if err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Vars map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := s.store.SetAppEnv(r.Context(), app.ID, req.Vars); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
```

- [ ] **Step 13: Register the routes**

In `internal/api/server.go`, inside `NewServer`, alongside the other
`handleAuth` calls:
```go
	srv.handleAuth("POST /apps/{name}/deploy", srv.handleManualDeploy)
	srv.handleAuth("PUT /apps/{name}/env", srv.handleSetEnv)
```

- [ ] **Step 14: Run tests to confirm they pass**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 15: Commit**

```bash
git add internal/store internal/deploy internal/api
git commit -m "Add app env vars and manual deploy endpoint"
```

---

### Task 11: Logs endpoint

**Files:**
- Modify: `internal/dockerx/client.go` (`apiClient` gains `ContainerLogs`)
- Modify: `internal/dockerx/containers.go` (new `Logs` method)
- Modify: `internal/dockerx/containers_test.go` (fake gains `ContainerLogs`)
- Modify: `internal/deploy/orchestrator.go` (`dockerAPI` gains `Logs`; new `Orchestrator.Logs`)
- Modify: `internal/deploy/orchestrator_test.go` (`fakeDocker` gains `Logs`)
- Modify: `internal/api/webhook_handler_test.go` (`webhookFakeDocker` gains `Logs`, `logsContent` field)
- Create: `internal/api/logs_handler_test.go`
- Modify: `internal/api/deploy_handlers.go` (new `handleGetLogs`)
- Modify: `internal/api/server.go` (register the route)

**Interfaces:**
- Produces:
  - `(*dockerx.Client).Logs(ctx, id string) (io.ReadCloser, error)`
  - `(*deploy.Orchestrator).Logs(ctx, appName string) (io.ReadCloser, error)`
  - `deploy.ErrNoContainer` — returned when the app has never been deployed
  - `GET /apps/{name}/logs` → 200 streaming the container's combined
    stdout/stderr; 404 if the app doesn't exist; 409 if it exists but
    has no container yet.

- [ ] **Step 1: Extend the dockerx fake and add `ContainerLogs` to the interface**

In `internal/dockerx/containers_test.go`, add to `fakeAPI`:
```go
func (f *fakeAPI) ContainerLogs(ctx context.Context, id string, options container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("log line 1\nlog line 2\n")), nil
}
```

Add this test to the same file:
```go
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
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/dockerx/... -run TestLogsReturnsContainerOutput`
Expected: FAIL (`Logs` undefined, `ContainerLogs` not on `apiClient`)

- [ ] **Step 3: Add `ContainerLogs` to the interface and implement `Logs`**

In `internal/dockerx/client.go`, add to the `apiClient` interface:
```go
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
```

In `internal/dockerx/containers.go`, add:
```go
func (c *Client) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	rc, err := c.api.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return nil, fmt.Errorf("logs for container %q: %w", id, err)
	}
	return rc, nil
}
```

- [ ] **Step 4: Run dockerx tests to confirm they pass**

Run: `go test ./internal/dockerx/...`
Expected: PASS

- [ ] **Step 5: Extend the deploy package's fake and interface**

In `internal/deploy/orchestrator_test.go`, add the `"io"` and
`"strings"` imports and this method on `fakeDocker`:
```go
func (f *fakeDocker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
```

Add this test:
```go
func TestLogsReturnsErrNoContainerBeforeFirstDeploy(t *testing.T) {
	ctx := context.Background()
	o, s := newTestOrchestrator(t, &fakeDocker{})
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	_, err := o.Logs(ctx, "myapp")
	if !errors.Is(err, ErrNoContainer) {
		t.Fatalf("expected ErrNoContainer, got %v", err)
	}
}
```

- [ ] **Step 6: Run to confirm it fails**

Run: `go test ./internal/deploy/... -run TestLogsReturnsErrNoContainerBeforeFirstDeploy`
Expected: FAIL (`Logs`/`ErrNoContainer` undefined)

- [ ] **Step 7: Add `Logs` to `dockerAPI` and implement `Orchestrator.Logs`**

In `internal/deploy/orchestrator.go`, add to the `dockerAPI` interface:
```go
	Logs(ctx context.Context, id string) (io.ReadCloser, error)
```

Add the import `"io"`, add `var ErrNoContainer = errors.New("app has no running container")`,
and add:
```go
func (o *Orchestrator) Logs(ctx context.Context, appName string) (io.ReadCloser, error) {
	app, err := o.store.GetAppByName(ctx, appName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}
	if app.ContainerID == "" {
		return nil, ErrNoContainer
	}
	return o.docker.Logs(ctx, app.ContainerID)
}
```

- [ ] **Step 8: Run deploy tests to confirm they pass**

Run: `go test ./internal/deploy/...`
Expected: PASS

- [ ] **Step 9: Extend the API package's webhook fake**

In `internal/api/webhook_handler_test.go`, add a `logsContent string`
field to `webhookFakeDocker` and this method:
```go
func (f *webhookFakeDocker) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.logsContent)), nil
}
```
Add the `"io"` and `"strings"` imports.

- [ ] **Step 10: Write the failing API test**

`internal/api/logs_handler_test.go`:
```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubeship/internal/deploy"
	"cubeship/internal/store"
)

func TestGetLogsStreamsContainerOutput(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	docker := &webhookFakeDocker{logsContent: "hello from the app\n"}
	srv := NewServer(s, deploy.New(s, docker), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "hello from the app\n" {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
}

func TestGetLogsBeforeFirstDeploy(t *testing.T) {
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	srv := NewServer(s, deploy.New(s, &webhookFakeDocker{}), "secret-token", "registry.example.com")

	req := authedRequest(http.MethodGet, "/apps/myapp/logs", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
}
```

- [ ] **Step 11: Run to confirm it fails**

Run: `go test ./internal/api/... -run TestGetLogs`
Expected: FAIL (route not registered)

- [ ] **Step 12: Implement the handler**

Add to `internal/api/deploy_handlers.go`:
```go
import (
	"errors"
	"io"

	"cubeship/internal/deploy"
)

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := s.store.GetAppByName(r.Context(), name); err != nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	rc, err := s.orch.Logs(r.Context(), name)
	if errors.Is(err, deploy.ErrNoContainer) {
		http.Error(w, "app has no running container yet", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rc.Close()

	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
}
```

Merge the new `errors`, `io`, and `cubeship/internal/deploy` imports
into the existing `import` block at the top of the file rather than
duplicating it.

- [ ] **Step 13: Register the route**

In `internal/api/server.go`, inside `NewServer`:
```go
	srv.handleAuth("GET /apps/{name}/logs", srv.handleGetLogs)
```

- [ ] **Step 14: Run the full test suite to confirm it passes**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 15: Commit**

```bash
git add internal/dockerx internal/deploy internal/api
git commit -m "Add container logs endpoint"
```

---

### Task 12: Startup reconciliation

**Files:**
- Create: `internal/reconcile/reconcile.go`
- Test: `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.App` (Task 2).
- Produces: `reconcile.Run(ctx context.Context, s *store.Store, d dockerAPI) error`

This runs once at daemon startup (wired in Task 13). For every app with
a recorded container, it checks whether that container is actually
running and corrects `apps.status` to match reality — it does not try
to restart anything. Per spec: "resolves to the last known-good
container" means the daemon trusts and reports the true Docker state
rather than trusting stale SQLite state, not that it takes recovery
action.

- [ ] **Step 1: Write the failing test**

`internal/reconcile/reconcile_test.go`:
```go
package reconcile

import (
	"context"
	"testing"

	"cubeship/internal/store"
)

type fakeDocker struct {
	running map[string]bool
}

func (f *fakeDocker) IsRunning(ctx context.Context, id string) (bool, error) {
	running, ok := f.running[id]
	if !ok {
		return false, nil
	}
	return running, nil
}

func TestReconcileMarksMissingContainerDown(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	docker := &fakeDocker{running: map[string]bool{}} // container-1 not found -> not running

	if err := Run(ctx, s, docker); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Status != "down" {
		t.Fatalf("expected status 'down', got %q", got.Status)
	}
	if got.ContainerID != "container-1" {
		t.Fatalf("expected container ID to be preserved for diagnosis, got %q", got.ContainerID)
	}
}

func TestReconcileConfirmsRunningContainer(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })

	app, _ := s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")
	s.UpdateAppContainer(ctx, app.ID, "container-1", "running")

	docker := &fakeDocker{running: map[string]bool{"container-1": true}}

	if err := Run(ctx, s, docker); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, _ := s.GetAppByName(ctx, "myapp")
	if got.Status != "running" {
		t.Fatalf("expected status to stay 'running', got %q", got.Status)
	}
}

func TestReconcileSkipsAppsNeverDeployed(t *testing.T) {
	ctx := context.Background()
	s, _ := store.Open(":memory:")
	t.Cleanup(func() { s.Close() })
	s.CreateApp(ctx, "myapp", "myapp.example.com", "registry.example.com/myapp")

	// Should not panic or error even though there's no container to check.
	if err := Run(ctx, s, &fakeDocker{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/reconcile/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/reconcile/reconcile.go`:
```go
package reconcile

import (
	"context"
	"log"

	"cubeship/internal/store"
)

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	IsRunning(ctx context.Context, id string) (bool, error)
}

// Run compares each app's recorded container against real Docker state
// and corrects apps.status to match reality. It never starts, stops,
// or removes a container.
func Run(ctx context.Context, s *store.Store, d dockerAPI) error {
	apps, err := s.ListApps(ctx)
	if err != nil {
		return err
	}

	for _, app := range apps {
		if app.ContainerID == "" {
			continue
		}
		running, err := d.IsRunning(ctx, app.ContainerID)
		if err != nil {
			log.Printf("reconcile: app %s: inspect container %s failed: %v", app.Name, app.ContainerID, err)
			running = false
		}

		wantStatus := "down"
		if running {
			wantStatus = "running"
		}
		if wantStatus != app.Status {
			log.Printf("reconcile: app %s: status %s -> %s", app.Name, app.Status, wantStatus)
			if err := s.UpdateAppContainer(ctx, app.ID, app.ContainerID, wantStatus); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/reconcile/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile
git commit -m "Add startup state reconciliation"
```

---

### Task 13: Config loading

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type config.Config struct { Domain, Token, DataDir, RegistryHost, APIHost, AcmeEmail string }`
  - `config.Load() (*Config, error)` — reads `CUBESHIP_DOMAIN` and
    `CUBESHIP_ACME_EMAIL` (both required — Let's Encrypt rejects
    certificate requests without a contact email), `CUBESHIP_TOKEN`
    (auto-generated if unset), `CUBESHIP_DATA_DIR` (defaults to
    `/var/lib/cubeship`).

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:
```go
package config

import "testing"

func TestLoadRequiresDomain(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when CUBESHIP_DOMAIN is unset")
	}
}

func TestLoadDerivesHostsFromDomain(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "fixed-token")
	t.Setenv("CUBESHIP_DATA_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RegistryHost != "registry.example.com" {
		t.Fatalf("expected registry.example.com, got %q", cfg.RegistryHost)
	}
	if cfg.APIHost != "api.example.com" {
		t.Fatalf("expected api.example.com, got %q", cfg.APIHost)
	}
	if cfg.Token != "fixed-token" {
		t.Fatalf("expected the provided token to be used, got %q", cfg.Token)
	}
	if cfg.DataDir != "/var/lib/cubeship" {
		t.Fatalf("expected the default data dir, got %q", cfg.DataDir)
	}
	if cfg.AcmeEmail != "admin@example.com" {
		t.Fatalf("expected the ACME email to be read, got %q", cfg.AcmeEmail)
	}
}

func TestLoadRequiresAcmeEmail(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when CUBESHIP_ACME_EMAIL is unset")
	}
}

func TestLoadGeneratesTokenWhenUnset(t *testing.T) {
	t.Setenv("CUBESHIP_DOMAIN", "example.com")
	t.Setenv("CUBESHIP_ACME_EMAIL", "admin@example.com")
	t.Setenv("CUBESHIP_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Token) != 64 {
		t.Fatalf("expected a 64-hex-char generated token, got %d chars: %q", len(cfg.Token), cfg.Token)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/config/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/config/config.go`:
```go
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

type Config struct {
	Domain       string
	Token        string
	DataDir      string
	RegistryHost string
	APIHost      string
	AcmeEmail    string
}

func Load() (*Config, error) {
	domain := os.Getenv("CUBESHIP_DOMAIN")
	if domain == "" {
		return nil, fmt.Errorf("CUBESHIP_DOMAIN environment variable is required")
	}

	acmeEmail := os.Getenv("CUBESHIP_ACME_EMAIL")
	if acmeEmail == "" {
		return nil, fmt.Errorf("CUBESHIP_ACME_EMAIL environment variable is required")
	}

	token := os.Getenv("CUBESHIP_TOKEN")
	if token == "" {
		generated, err := generateToken()
		if err != nil {
			return nil, fmt.Errorf("generate token: %w", err)
		}
		token = generated
	}

	dataDir := os.Getenv("CUBESHIP_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/cubeship"
	}

	return &Config{
		Domain:       domain,
		Token:        token,
		DataDir:      dataDir,
		RegistryHost: "registry." + domain,
		APIHost:      "api." + domain,
		AcmeEmail:    acmeEmail,
	}, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/config/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "Add daemon config loading"
```

---

### Task 14: Extend dockerx for ports, mounts, and networks

Traefik (bootstrapped in Task 15) needs published ports, a bind mount
for the Docker socket, command-line flags, and a shared Docker network
— none of which `ContainerOpts` (Task 4) currently supports. This task
extends it.

**Files:**
- Modify: `internal/dockerx/client.go` (`apiClient` gains `NetworkCreate`)
- Modify: `internal/dockerx/containers.go` (`ContainerOpts` gains `Cmd`,
  `Binds`, `Ports`, `Network`; new `EnsureNetwork`)
- Modify: `internal/dockerx/containers_test.go` (fake gains
  `NetworkCreate`; new assertions)

**Interfaces:**
- Produces:
  - `ContainerOpts.Cmd []string`, `.Binds []string` (`"host:container[:ro]"`),
    `.Ports []string` (`"hostPort:containerPort"`), `.Network string`,
    `.HostNetwork bool`
  - `(*Client).EnsureNetwork(ctx, name string) error` — idempotent;
    logs and ignores "already exists" style errors.

`HostNetwork` and `Network`/`Ports` are mutually exclusive: when
`HostNetwork` is true, the container shares the VPS's network stack
directly (`localhost` reaches the daemon; container IPs on the Docker
bridge are still reachable from the host without needing published
ports), and `Ports`/`Network` are ignored. This is how Traefik is
bootstrapped in Task 15 — it's the only container that needs to reach
both the host-process daemon and other containers.

- [ ] **Step 1: Extend the fake and write the failing test**

Add to `internal/dockerx/containers_test.go`:
```go
func (f *fakeAPI) NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{ID: "net-1"}, nil
}
```

Add these tests:
```go
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

func TestEnsureNetworkIgnoresErrors(t *testing.T) {
	fake := &fakeAPI{}
	c := newWithAPI(fake)

	if err := c.EnsureNetwork(context.Background(), "cubeship"); err != nil {
		t.Fatalf("EnsureNetwork: %v", err)
	}
}
```

Also add `createdHostConfig *container.HostConfig` and
`createdNetworkingConfig *network.NetworkingConfig` fields to `fakeAPI`,
and capture them in `ContainerCreate`:
```go
func (f *fakeAPI) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	f.createdConfig = config
	f.createdHostConfig = hostConfig
	f.createdNetworkingConfig = networkingConfig
	f.createdName = containerName
	return container.CreateResponse{ID: "new-container-id"}, nil
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/dockerx/...`
Expected: FAIL (`Cmd`/`Binds`/`Ports`/`Network`/`EnsureNetwork` undefined)

- [ ] **Step 3: Implement**

In `internal/dockerx/client.go`, add to the `apiClient` interface:
```go
	NetworkCreate(ctx context.Context, name string, options network.CreateOptions) (network.CreateResponse, error)
```

In `internal/dockerx/containers.go`, replace `ContainerOpts` and
`CreateContainer`, and add `EnsureNetwork`:
```go
type ContainerOpts struct {
	Name        string
	Image       string
	Labels      map[string]string
	Env         []string
	Cmd         []string
	Binds       []string
	Ports       []string
	Network     string
	HostNetwork bool
}

func (c *Client) CreateContainer(ctx context.Context, opts ContainerOpts) (string, error) {
	var exposedPorts nat.PortSet
	var portBindings nat.PortMap
	var networkMode container.NetworkMode
	var networkingConfig *network.NetworkingConfig

	if opts.HostNetwork {
		networkMode = "host"
	} else {
		var err error
		exposedPorts, portBindings, err = nat.ParsePortSpecs(opts.Ports)
		if err != nil {
			return "", fmt.Errorf("parse port specs for %q: %w", opts.Name, err)
		}
		if opts.Network != "" {
			networkingConfig = &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					opts.Network: {},
				},
			}
		}
	}

	resp, err := c.api.ContainerCreate(ctx,
		&container.Config{
			Image:        opts.Image,
			Labels:       opts.Labels,
			Env:          opts.Env,
			Cmd:          opts.Cmd,
			ExposedPorts: exposedPorts,
		},
		&container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
			Binds:         opts.Binds,
			PortBindings:  portBindings,
			NetworkMode:   networkMode,
		},
		networkingConfig, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("create container %q: %w", opts.Name, err)
	}
	return resp.ID, nil
}

func (c *Client) EnsureNetwork(ctx context.Context, name string) error {
	if _, err := c.api.NetworkCreate(ctx, name, network.CreateOptions{}); err != nil {
		log.Printf("dockerx: create network %q: %v (assuming it already exists)", name, err)
	}
	return nil
}
```

Add `"log"` and `"github.com/docker/docker/api/types/nat"` to the
imports of `containers.go`.

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/dockerx/...`
Expected: PASS. As in Task 4, if the installed SDK's `nat`/`network`
package shapes differ, follow the compiler errors to the real field
names.

- [ ] **Step 5: Commit**

```bash
git add internal/dockerx
git commit -m "Extend dockerx for ports, binds, and networks"
```

---

### Task 15: Bootstrap — registry and Traefik container specs

**Files:**
- Create: `internal/bootstrap/bootstrap.go`
- Test: `internal/bootstrap/bootstrap_test.go`

**Interfaces:**
- Consumes: `config.Config` (Task 13), `dockerx.ContainerOpts` (Tasks 4, 14), `traefik.Labels` (Task 5).
- Produces:
  - `bootstrap.RegistryContainerOpts(cfg *config.Config, notifyURL string) dockerx.ContainerOpts`
  - `bootstrap.TraefikContainerOpts(cfg *config.Config, acmeEmail string) dockerx.ContainerOpts`
  - `bootstrap.Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error`
    — pulls the image, creates the container, starts it; if creation
    fails (most likely: the name is already in use from a previous run)
    it logs and returns nil rather than erroring, since "already
    running" is the desired end state either way.

- [ ] **Step 1: Write the failing test**

`internal/bootstrap/bootstrap_test.go`:
```go
package bootstrap

import (
	"context"
	"errors"
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
	if len(opts.Binds) != 2 {
		t.Fatalf("expected docker socket + acme storage binds, got %v", opts.Binds)
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
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/bootstrap/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/bootstrap/bootstrap.go`:
```go
package bootstrap

import (
	"context"
	"fmt"
	"log"

	"cubeship/internal/config"
	"cubeship/internal/dockerx"
	"cubeship/internal/traefik"
)

const registryPort = 5000

func RegistryContainerOpts(cfg *config.Config, notifyURL string) dockerx.ContainerOpts {
	labels := traefik.Labels("registry", cfg.RegistryHost, registryPort)
	return dockerx.ContainerOpts{
		Name:   "cubeship-registry",
		Image:  "registry:2",
		Labels: labels,
		Env: []string{
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_NAME=cubeshipd",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_URL=" + notifyURL,
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_TIMEOUT=5s",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_THRESHOLD=5",
			"REGISTRY_NOTIFICATIONS_ENDPOINTS_0_BACKOFF=1s",
		},
		Network: "cubeship",
		// Also published on localhost, plain HTTP, bypassing Traefik/TLS.
		// Docker trusts 127.0.0.0/8 as insecure-by-default, so this needs
		// no daemon.json changes — useful for local pushes and is how
		// Task 20's integration test pushes without needing a real
		// public domain for ACME.
		Ports: []string{"127.0.0.1:5000:5000"},
	}
}

func TraefikContainerOpts(cfg *config.Config, acmeEmail string) dockerx.ContainerOpts {
	return dockerx.ContainerOpts{
		Name:  "cubeship-traefik",
		Image: "traefik:v3.1",
		Cmd: []string{
			"--providers.docker=true",
			"--providers.docker.exposedbydefault=false",
			"--entrypoints.web.address=:80",
			"--entrypoints.websecure.address=:443",
			"--certificatesresolvers.letsencrypt.acme.tlschallenge=true",
			"--certificatesresolvers.letsencrypt.acme.email=" + acmeEmail,
			"--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json",
			"--api.dashboard=false",
		},
		Binds: []string{
			"/var/run/docker.sock:/var/run/docker.sock:ro",
			cfg.DataDir + "/letsencrypt:/letsencrypt",
		},
		HostNetwork: true,
	}
}

// dockerAPI is the subset of dockerx.Client this package needs.
type dockerAPI interface {
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, opts dockerx.ContainerOpts) (string, error)
	StartContainer(ctx context.Context, id string) error
}

func Ensure(ctx context.Context, docker dockerAPI, opts dockerx.ContainerOpts) error {
	if err := docker.PullImage(ctx, opts.Image); err != nil {
		return fmt.Errorf("pull %s: %w", opts.Image, err)
	}

	id, err := docker.CreateContainer(ctx, opts)
	if err != nil {
		log.Printf("bootstrap: create %s: %v (assuming it already exists and is running)", opts.Name, err)
		return nil
	}

	if err := docker.StartContainer(ctx, id); err != nil {
		return fmt.Errorf("start %s: %w", opts.Name, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/bootstrap/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap
git commit -m "Add registry and Traefik container bootstrap specs"
```

---

### Task 16: Daemon composition root

**Files:**
- Modify: `cmd/cubeshipd/main.go` (replace the stub from Task 1)

**Interfaces:**
- Consumes: every package from Tasks 2-15.

This is the wiring that turns the tested-in-isolation packages into a
running daemon. There is no pure logic here to unit test — it opens a
real Docker connection, real SQLite file, and binds a real port — so
this task is verified by `go build` and by Task 17 (CLI) being able to
talk to a daemon started from this binary, not by a unit test.

- [ ] **Step 1: Implement**

`cmd/cubeshipd/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"cubeship/internal/api"
	"cubeship/internal/bootstrap"
	"cubeship/internal/config"
	"cubeship/internal/deploy"
	"cubeship/internal/dockerx"
	"cubeship/internal/reconcile"
	"cubeship/internal/store"
)

const version = "0.1.0-dev"
const listenAddr = ":9000"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cubeshipd %s\n", version)
		os.Exit(0)
	}

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log.Printf("cubeshipd starting for domain %s", cfg.Domain)
	log.Printf("daemon API token: %s", cfg.Token)

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	docker, err := dockerx.New()
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}

	ctx := context.Background()

	if err := docker.EnsureNetwork(ctx, "cubeship"); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	s, err := store.Open(cfg.DataDir + "/cubeship.db")
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer s.Close()

	notifyURL := "http://127.0.0.1" + listenAddr + "/hooks/registry"
	if err := bootstrap.Ensure(ctx, docker, bootstrap.RegistryContainerOpts(cfg, notifyURL)); err != nil {
		return fmt.Errorf("bootstrap registry: %w", err)
	}
	if err := bootstrap.Ensure(ctx, docker, bootstrap.TraefikContainerOpts(cfg, cfg.AcmeEmail)); err != nil {
		return fmt.Errorf("bootstrap traefik: %w", err)
	}

	if err := reconcile.Run(ctx, s, docker); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	orch := deploy.NewOrchestrator(s, docker)
	srv := api.NewServer(s, orch, cfg.Token, cfg.RegistryHost)

	log.Printf("cubeshipd listening on %s", listenAddr)
	return http.ListenAndServe(listenAddr, srv.Router())
}
```

- [ ] **Step 2: Confirm the whole module builds**

Run: `go build ./...`
Expected: succeeds with no errors.

- [ ] **Step 3: Run the full test suite one more time**

Run: `go test ./...`
Expected: PASS across every package.

- [ ] **Step 4: Commit**

```bash
git add cmd/cubeshipd
git commit -m "Wire the daemon composition root"
```

---

### Task 17: CLI API client

**Files:**
- Create: `internal/apiclient/client.go`
- Test: `internal/apiclient/client_test.go`

**Interfaces:**
- Produces:
  - `apiclient.New(baseURL, token string) *Client`
  - `(*Client).CreateApp(ctx, name, domain string) (image string, err error)`
  - `(*Client).Deploy(ctx, name, tag string) error`
  - `(*Client).SetEnv(ctx, name string, vars map[string]string) error`
  - `(*Client).Logs(ctx, name string) (io.ReadCloser, error)`

This is a plain HTTP client for the daemon API built in Tasks 8-11.
It's tested against a real `httptest.Server`, not a mock, since the
contract under test *is* the HTTP wire format.

- [ ] **Step 1: Write the failing test**

`internal/apiclient/client_test.go`:
```go
package apiclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAppSendsAuthAndReturnsImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodPost || r.URL.Path != "/apps" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "myapp" || body["domain"] != "myapp.example.com" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"image": "registry.example.com/myapp"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	image, err := c.CreateApp(context.Background(), "myapp", "myapp.example.com")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.example.com/myapp" {
		t.Fatalf("expected image registry.example.com/myapp, got %q", image)
	}
}

func TestDeploySendsTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/myapp/deploy" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["tag"] != "v2" {
			t.Errorf("expected tag v2, got %v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.Deploy(context.Background(), "myapp", "v2"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
}

func TestDeployReturnsErrorOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]string{"error": "health check timed out"})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	err := c.Deploy(context.Background(), "myapp", "latest")
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestSetEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/apps/myapp/env" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	if err := c.SetEnv(context.Background(), "myapp", map[string]string{"PORT": "8080"}); err != nil {
		t.Fatalf("SetEnv: %v", err)
	}
}

func TestLogsStreamsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from the app\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-token")
	rc, err := c.Logs(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "hello from the app\n" {
		t.Fatalf("unexpected log output: %q", data)
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/apiclient/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/apiclient/client.go`:
```go
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	return c.http.Do(req)
}

func (c *Client) CreateApp(ctx context.Context, name, domain string) (string, error) {
	resp, err := c.do(ctx, http.MethodPost, "/apps", map[string]string{"name": name, "domain": domain})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create app: unexpected status %d", resp.StatusCode)
	}
	var out struct {
		Image string `json:"image"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Image, nil
}

func (c *Client) Deploy(ctx context.Context, name, tag string) error {
	resp, err := c.do(ctx, http.MethodPost, "/apps/"+name+"/deploy", map[string]string{"tag": tag})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deploy: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SetEnv(ctx context.Context, name string, vars map[string]string) error {
	resp, err := c.do(ctx, http.MethodPut, "/apps/"+name+"/env", map[string]map[string]string{"vars": vars})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("set env: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Logs(ctx context.Context, name string) (io.ReadCloser, error) {
	resp, err := c.do(ctx, http.MethodGet, "/apps/"+name+"/logs", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, fmt.Errorf("logs: unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/apiclient/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apiclient
git commit -m "Add CLI HTTP client for the daemon API"
```

---

### Task 18: CLI credentials storage

**Files:**
- Create: `internal/clicreds/clicreds.go`
- Test: `internal/clicreds/clicreds_test.go`

**Interfaces:**
- Produces:
  - `type clicreds.Credentials struct { BaseURL string; Token string }`
  - `clicreds.Save(path string, creds Credentials) error`
  - `clicreds.Load(path string) (Credentials, error)`
  - `clicreds.DefaultPath() (string, error)` — `$HOME/.config/cubeship/credentials.json`
  - `clicreds.RegistryHostFromBaseURL(baseURL string) (string, error)` —
    the daemon's API is reached at `https://api.<domain>` (Task 13); the
    registry is `registry.<domain>`. This derives the latter from the
    former so `registry login` doesn't need a second piece of config.

- [ ] **Step 1: Write the failing test**

`internal/clicreds/clicreds_test.go`:
```go
package clicreds

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")
	want := Credentials{BaseURL: "https://api.example.com", Token: "secret-token"}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("expected an error for a missing credentials file")
	}
}

func TestRegistryHostFromBaseURL(t *testing.T) {
	got, err := RegistryHostFromBaseURL("https://api.example.com")
	if err != nil {
		t.Fatalf("RegistryHostFromBaseURL: %v", err)
	}
	if got != "registry.example.com" {
		t.Fatalf("expected registry.example.com, got %q", got)
	}
}

func TestRegistryHostFromBaseURLRejectsUnexpectedHost(t *testing.T) {
	_, err := RegistryHostFromBaseURL("https://cubeship.example.com")
	if err == nil {
		t.Fatal("expected an error for a base URL that isn't api.<domain>")
	}
}
```

- [ ] **Step 2: Run to confirm it fails**

Run: `go test ./internal/clicreds/...`
Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Implement**

`internal/clicreds/clicreds.go`:
```go
package clicreds

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Credentials struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

func Save(path string, creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Load(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials (run 'cubeship login' first): %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	return creds, nil
}

func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cubeship", "credentials.json"), nil
}

func RegistryHostFromBaseURL(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	if !strings.HasPrefix(u.Hostname(), "api.") {
		return "", fmt.Errorf("expected base URL host to start with 'api.', got %q", u.Hostname())
	}
	return "registry." + strings.TrimPrefix(u.Hostname(), "api."), nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./internal/clicreds/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/clicreds
git commit -m "Add CLI credentials storage"
```

---

### Task 19: CLI commands

**Files:**
- Modify: `cmd/cubeship/main.go` (replace the stub from Task 1 with a cobra root)
- Create: `cmd/cubeship/login.go`
- Create: `cmd/cubeship/app.go`
- Modify: `go.mod` (add `github.com/spf13/cobra`)

**Interfaces:**
- Consumes: `apiclient` (Task 17), `clicreds` (Task 18).
- Produces the commands from the spec's Components section:
  `login`, `registry login`, `app create`, `app deploy`, `app logs`,
  `app env set`.

This task is glue over already-tested packages (network calls and file
I/O happen for real) — verified by `go build` plus the end-to-end
integration test in Task 20, not by unit tests here.

- [ ] **Step 1: Add the cobra dependency**

```bash
go get github.com/spf13/cobra@v1.8.1
```

- [ ] **Step 2: Implement the root command**

`cmd/cubeship/main.go`:
```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:     "cubeship",
		Short:   "CLI for the Cubeship self-hosted deploy engine",
		Version: version,
	}

	root.AddCommand(newLoginCmd())
	root.AddCommand(newRegistryCmd())
	root.AddCommand(newAppCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Implement `login` and `registry login`**

`cmd/cubeship/login.go`:
```go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"cubeship/internal/clicreds"

	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login <daemon-url> <token>",
		Short: "Save credentials for talking to a Cubeship daemon",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := clicreds.DefaultPath()
			if err != nil {
				return err
			}
			creds := clicreds.Credentials{BaseURL: strings.TrimRight(args[0], "/"), Token: args[1]}
			if err := clicreds.Save(path, creds); err != nil {
				return err
			}
			fmt.Printf("Saved credentials to %s\n", path)
			return nil
		},
	}
}

func newRegistryCmd() *cobra.Command {
	registryCmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage the Cubeship container registry",
	}
	registryCmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Run 'docker login' against the Cubeship registry",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := clicreds.DefaultPath()
			if err != nil {
				return err
			}
			creds, err := clicreds.Load(path)
			if err != nil {
				return err
			}
			registryHost, err := clicreds.RegistryHostFromBaseURL(creds.BaseURL)
			if err != nil {
				return err
			}

			dockerLogin := exec.Command("docker", "login", registryHost, "-u", "cubeship", "--password-stdin")
			dockerLogin.Stdin = strings.NewReader(creds.Token)
			dockerLogin.Stdout = os.Stdout
			dockerLogin.Stderr = os.Stderr
			return dockerLogin.Run()
		},
	})
	return registryCmd
}
```

- [ ] **Step 4: Implement `app` commands**

`cmd/cubeship/app.go`:
```go
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"cubeship/internal/apiclient"
	"cubeship/internal/clicreds"

	"github.com/spf13/cobra"
)

func newAPIClient() (*apiclient.Client, error) {
	path, err := clicreds.DefaultPath()
	if err != nil {
		return nil, err
	}
	creds, err := clicreds.Load(path)
	if err != nil {
		return nil, err
	}
	return apiclient.New(creds.BaseURL, creds.Token), nil
}

func newAppCmd() *cobra.Command {
	appCmd := &cobra.Command{Use: "app", Short: "Manage Cubeship apps"}

	var domain string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Register a new app and get its registry image path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			image, err := c.CreateApp(context.Background(), args[0], domain)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s. Push to: %s\n", args[0], image)
			return nil
		},
	}
	createCmd.Flags().StringVar(&domain, "domain", "", "domain the app will be served on")
	createCmd.MarkFlagRequired("domain")

	var tag string
	deployCmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Manually redeploy an app from the given (or latest) image tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.Deploy(context.Background(), args[0], tag); err != nil {
				return err
			}
			fmt.Printf("Deployed %s\n", args[0])
			return nil
		},
	}
	deployCmd.Flags().StringVar(&tag, "tag", "latest", "image tag to deploy")

	logsCmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Stream an app's container logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			rc, err := c.Logs(context.Background(), args[0])
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(os.Stdout, rc)
			return err
		},
	}

	envCmd := &cobra.Command{Use: "env", Short: "Manage an app's environment variables"}
	envSetCmd := &cobra.Command{
		Use:   "set <name> KEY=VALUE [KEY=VALUE...]",
		Short: "Set environment variables for an app",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars := map[string]string{}
			for _, kv := range args[1:] {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid KEY=VALUE pair: %q", kv)
				}
				vars[parts[0]] = parts[1]
			}
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			if err := c.SetEnv(context.Background(), args[0], vars); err != nil {
				return err
			}
			fmt.Printf("Updated env for %s\n", args[0])
			return nil
		},
	}
	envCmd.AddCommand(envSetCmd)

	appCmd.AddCommand(createCmd, deployCmd, logsCmd, envCmd)
	return appCmd
}
```

- [ ] **Step 5: Confirm the module builds**

Run: `go build ./...`
Expected: succeeds with no errors.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./...`
Expected: PASS across every package.

- [ ] **Step 7: Commit**

```bash
git add cmd/cubeship go.mod go.sum
git commit -m "Add CLI commands for login, registry login, and app management"
```

---

### Task 20: End-to-end integration test

**Files:**
- Create: `test/integration/testapp/Dockerfile`
- Create: `test/integration/deploy_test.go`

**Interfaces:**
- Consumes: the built `cubeshipd` binary (Task 16), `apiclient` (Task 17).

**Prerequisites this test needs (documented, not automated):** Docker
running and reachable from the test host; the ability to bind ports 80
and 443 (run as root, or with `CAP_NET_BIND_SERVICE` on Linux); outbound
internet access to resolve DNS for `localtest.me` (a public service
whose `*.localtest.me` records point at `127.0.0.1` — this lets the test
exercise real Traefik `Host()` routing without owning a real domain).
Gated behind the `integration` build tag so `go test ./...` never runs
it by accident.

**What this does and does not prove:** it proves the full wire-up —
daemon bootstraps registry + Traefik, a registry push fires the
webhook, the orchestrator deploys the container, and Traefik routes the
configured domain to it. It does **not** prove real Let's Encrypt
issuance succeeds — that requires a real public domain and port 80
reachable from Let's Encrypt's validators, which is inherently outside
what an automated test can exercise. The test accepts Traefik's
self-signed fallback certificate (`InsecureSkipVerify`) for that reason.

- [ ] **Step 1: Add the fixture image**

`test/integration/testapp/Dockerfile`:
```dockerfile
FROM hashicorp/http-echo
CMD ["-listen=:8080", "-text=hello from cubeship"]
```

- [ ] **Step 2: Write the test**

`test/integration/deploy_test.go`:
```go
//go:build integration

package integration

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"cubeship/internal/apiclient"
)

const testToken = "integration-test-token"

func waitFor(t *testing.T, timeout time.Duration, desc string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", desc)
}

func TestDeployEndToEnd(t *testing.T) {
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	daemonBin := filepath.Join(t.TempDir(), "cubeshipd")
	build := exec.Command("go", "build", "-o", daemonBin, "./cmd/cubeshipd")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build daemon: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	daemon := exec.Command(daemonBin)
	daemon.Env = append(os.Environ(),
		"CUBESHIP_DOMAIN=localtest.me",
		"CUBESHIP_ACME_EMAIL=test@example.com",
		"CUBESHIP_TOKEN="+testToken,
		"CUBESHIP_DATA_DIR="+dataDir,
	)
	daemon.Stdout = os.Stdout
	daemon.Stderr = os.Stderr
	if err := daemon.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() {
		daemon.Process.Kill()
		daemon.Wait()
		exec.Command("docker", "rm", "-f", "cubeship-registry", "cubeship-traefik").Run()
		exec.Command("sh", "-c", "docker rm -f $(docker ps -aq --filter name=cubeship-myapp-)").Run()
	})

	waitFor(t, 30*time.Second, "daemon healthz", func() bool {
		resp, err := http.Get("http://127.0.0.1:9000/healthz")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	waitFor(t, 60*time.Second, "registry reachable on localhost:5000", func() bool {
		resp, err := http.Get("http://127.0.0.1:5000/v2/")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})

	ctx := context.Background()
	client := apiclient.New("http://127.0.0.1:9000", testToken)

	image, err := client.CreateApp(ctx, "myapp", "myapp.localtest.me")
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if image != "registry.localtest.me/myapp" {
		t.Fatalf("unexpected image: %q", image)
	}

	buildApp := exec.Command("docker", "build", "-t", "localhost:5000/myapp:latest", "./testapp")
	if out, err := buildApp.CombinedOutput(); err != nil {
		t.Fatalf("build fixture image: %v\n%s", err, out)
	}
	push := exec.Command("docker", "push", "localhost:5000/myapp:latest")
	if out, err := push.CombinedOutput(); err != nil {
		t.Fatalf("push fixture image: %v\n%s", err, out)
	}

	waitFor(t, 60*time.Second, "app deployed after push", func() bool {
		req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:9000/apps/myapp", nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var app struct {
			Status string `json:"status"`
		}
		if resp.StatusCode != http.StatusOK {
			return false
		}
		jsonDecodeOrFatal(t, resp, &app)
		return app.Status == "running"
	})

	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   10 * time.Second,
	}

	var body []byte
	waitFor(t, 30*time.Second, "app reachable via Traefik", func() bool {
		resp, err := httpClient.Get("https://myapp.localtest.me/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		body = buf[:n]
		return resp.StatusCode == http.StatusOK
	})

	if string(body) != "hello from cubeship\n" {
		t.Fatalf("unexpected response body from the deployed app: %q", body)
	}
}

func jsonDecodeOrFatal(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := jsonDecode(resp, v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
```

- [ ] **Step 3: Add the small JSON decode helper used above**

Add to `test/integration/deploy_test.go`, above `jsonDecodeOrFatal`:
```go
func jsonDecode(resp *http.Response, v any) error {
	return json.NewDecoder(resp.Body).Decode(v)
}
```
Add `"encoding/json"` to the imports.

- [ ] **Step 4: Run it**

Run: `go test -tags integration ./test/integration/... -v -timeout 5m`
Expected: PASS. If it fails at the "registry reachable" or "daemon
healthz" wait, check the daemon's stdout/stderr (printed inline by the
test) for the actual bootstrap error — the most common cause is ports
80/443/5000 already in use, or the test process lacking permission to
bind 80/443.

- [ ] **Step 5: Commit**

```bash
git add test/integration
git commit -m "Add end-to-end integration test for the core deploy loop"
```

---

## Plan Self-Review Notes

- **Spec coverage:** push-triggers-deploy (Tasks 6, 9), custom
  domain + HTTPS via Traefik labels (Tasks 5, 6, 15), zero-downtime
  swap and failed-deploy-leaves-old-container-running (Task 6), CLI +
  HTTP API both first-class (Tasks 8-11 API, 17-19 CLI), startup
  reconciliation (Task 12), unmatched-repository-push ignored (Task 9),
  cert-issuance-failure-doesn't-block-deploy (inherent: Traefik issues
  certs independently of container routing — no task blocks a deploy on
  cert status), unit tests with mocked Docker (every `internal/*` task),
  integration test proving the real wire-up (Task 20).
- **Deferred to later sub-projects, confirmed absent here:** building
  from Git/Dockerfile, managed databases, web UI, multi-tenant auth —
  matches the spec's Non-goals.
- **Convention introduced beyond the spec, called out explicitly:** app
  containers are assumed to listen on port 8080 (Task 6) — the spec
  doesn't define a port story for sub-project 1, and a per-app
  configurable port is out of scope here.

