// Package servertest builds a fully wired Cubeship server against a
// throwaway database, for tests that exercise a module through its real
// HTTP or MCP surface rather than through its service directly.
//
// A module's own tests import it from an external test package
// (package app_test, say), which is what keeps this from being an import
// cycle: servertest depends on server, which depends on every module.
package servertest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cubeship/internal/app"
	"cubeship/internal/platform/authkey"
	"cubeship/internal/platform/database"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/httpx"
	"cubeship/internal/project"
	"cubeship/internal/server"
	"cubeship/internal/settings"
	"cubeship/internal/setup"
	"cubeship/internal/user"
)

// WebhookToken is the shared secret the test server accepts registry
// notifications with.
const WebhookToken = "webhook-secret"

// LocalRegistry stands in for wherever the daemon pulls an app's own
// image from. Which address that is depends on whether the daemon is a
// container or a host process, and no test here depends on which.
const LocalRegistry = "127.0.0.1:5000"

// Domain is the base domain the fixture configures, and the two names
// derived from it. A real instance starts with no domain at all; the
// fixture sets one because most tests are about what happens after it is
// configured.
const (
	// The instance's own name, which is what an install is offered:
	// a subdomain the operator hands over whole, with everything
	// Cubeship serves living under it.
	Domain       = "cubeship.example.com"
	RegistryHost = "registry." + Domain
	// The daemon answers at the domain itself. The dashboard and the API
	// are one server at one address, so there is no second name.
	APIHost = Domain
)

// Fixture is a running server plus the identities and scopes a test
// needs to reach it.
type Fixture struct {
	Server *server.Server
	DB     *database.DB

	// DataDir is the instance's state directory, a temporary one per
	// fixture. Traefik's certificate store lives under it, so a test
	// about certificates writes one there.
	DataDir string

	// Admin holds the admin role, and AdminKey is their API key. Use it
	// to set up whatever a test needs; use AddMember to test
	// authorization.
	Admin    *user.User
	AdminKey string

	// Project and Environment are a ready-made scope for apps:
	// "web" / "production".
	Project     *project.Project
	Environment *project.Environment
}

// New returns a server wired over an empty database, with an admin
// account and a "web" project.
//
// Docker is a stub that refuses every operation, so a test that deploys
// without meaning to gets a failed deployment rather than a panic in a
// background goroutine. Pass a real fake to NewWithDocker to exercise a
// deploy.
func New(t testing.TB) *Fixture {
	t.Helper()
	return NewWithDocker(t, noDocker{})
}

// noDocker stands in for a Docker daemon that isn't there.
type noDocker struct{}

var errNoDocker = errors.New("this test has no Docker configured")

func (noDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error { return errNoDocker }
func (noDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "", errNoDocker
}
func (noDocker) StartContainer(context.Context, string) error  { return errNoDocker }
func (noDocker) StopContainer(context.Context, string) error   { return errNoDocker }
func (noDocker) RemoveContainer(context.Context, string) error { return errNoDocker }
func (noDocker) IsRunning(context.Context, string) (bool, error) {
	return false, errNoDocker
}

func (noDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return nil, errNoDocker
}

// NewEmpty is a server with no account at all — the state an instance is
// in between installing and someone claiming it.
func NewEmpty(t testing.TB) *Fixture {
	t.Helper()
	db := dbtest.New(t)
	dataDir := t.TempDir()
	return &Fixture{
		Server: server.New(db, noDocker{}, server.Options{
			WebhookToken: WebhookToken, LocalRegistry: LocalRegistry, DataDir: dataDir,
		}),
		DB:      db,
		DataDir: dataDir,
	}
}

// NewUnclaimed is NewEmpty with the setup token a real daemon writes on
// first start, which is the state an instance is actually in between
// installing and someone claiming it.
func NewUnclaimed(t testing.TB, token setup.Token) *Fixture {
	t.Helper()
	db := dbtest.New(t)
	dataDir := t.TempDir()
	return &Fixture{
		Server: server.New(db, noDocker{}, server.Options{
			WebhookToken: WebhookToken, LocalRegistry: LocalRegistry,
			SetupToken: token, DataDir: dataDir,
		}),
		DB:      db,
		DataDir: dataDir,
	}
}

// NewUnconfigured is New without a domain — the state a fresh install is
// in, before anyone has been to the settings page.
func NewUnconfigured(t testing.TB) *Fixture {
	t.Helper()
	return newFixture(t, noDocker{}, "")
}

// NewWithDocker is New with a Docker client (usually a fake) wired into
// the deploy orchestrator.
func NewWithDocker(t testing.TB, docker app.DockerAPI) *Fixture {
	t.Helper()
	return newFixture(t, docker, Domain)
}

func newFixture(t testing.TB, docker app.DockerAPI, domain string) *Fixture {
	t.Helper()
	ctx := context.Background()
	db := dbtest.New(t)

	dataDir := t.TempDir()
	srv := server.New(db, docker, server.Options{
		WebhookToken: WebhookToken, LocalRegistry: LocalRegistry, DataDir: dataDir,
	})
	if domain != "" {
		if err := srv.Settings.SeedFromEnv(ctx, map[string]string{settings.Domain: domain}); err != nil {
			t.Fatalf("configure the fixture's domain: %v", err)
		}
	}

	admin, adminKey := CreateUser(t, db, "admin", user.RoleAdmin)
	p, env, err := srv.Projects.Create(ctx, admin, "web")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &Fixture{
		Server: srv, DB: db, DataDir: dataDir,
		Admin: admin, AdminKey: adminKey,
		Project: p, Environment: env,
	}
}

// CreateUser adds a user directly to the database and returns them with a
// fresh API key. It bypasses the API on purpose: a test that needs an
// identity should not have to succeed at creating one first, and on a
// real instance the only account creation that needs no account is
// setup, which most tests are not about.
func CreateUser(t testing.TB, db *database.DB, username string, role user.Role) (*user.User, string) {
	t.Helper()
	ctx := context.Background()
	repo := user.NewRepository(db)

	u, err := repo.Create(ctx, username, role)
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	key, err := authkey.Generate()
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if _, err := repo.CreateAPIKey(ctx, u.ID, authkey.Hash(key), user.DefaultAPIKeyName); err != nil {
		t.Fatalf("create api key for %q: %v", username, err)
	}
	return u, key
}

// AddMember creates an account with the given role and returns their
// API key.
func (f *Fixture) AddMember(t testing.TB, username string, role user.Role) (*user.User, string) {
	t.Helper()
	return CreateUser(t, f.DB, username, role)
}

// Login signs in through the real endpoint and returns the session
// cookie, for a test that wants to act as a browser rather than as a CLI.
func (f *Fixture) Login(t testing.TB, username, password string) *http.Cookie {
	t.Helper()

	rec := f.Do(t, http.MethodPost, "/auth/login",
		map[string]string{"username": username, "password": password}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("sign in as %q: %d %s", username, rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == user.SessionCookieName {
			return cookie
		}
	}
	t.Fatalf("no %s cookie in the sign-in response", user.SessionCookieName)
	return nil
}

// DoAs sends a request carrying a session cookie instead of an API key.
func (f *Fixture) DoAs(t testing.TB, method, path string, body any, session *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := f.request(t, method, path, body, "")
	if session != nil {
		req.AddCookie(session)
	}
	// What a browser sends for a request its own page made. The
	// middleware refuses a cookie without it — see httpx.SameOrigin —
	// so a fixture that acts as a browser has to look like one.
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	f.Server.Router().ServeHTTP(rec, req)
	return rec
}

// Do sends an authenticated request through the server's router and
// returns the recorded response. body may be nil, a []byte, or any value
// to be JSON-encoded.
func (f *Fixture) Do(t testing.TB, method, path string, body any, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.Server.Router().ServeHTTP(rec, f.request(t, method, path, body, apiKey))
	return rec
}

func (f *Fixture) request(t testing.TB, method, path string, body any, apiKey string) *http.Request {
	t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		encoded, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	// Tests name API routes the way the modules register them, without
	// the prefix the router adds. One place applies it, so a test reads
	// as the route it is exercising.
	req := httptest.NewRequest(method, httpx.APIPrefix+path, reader)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

// DoJSON is Do plus decoding a successful response body into out.
// DoRoot drives a route that lives outside the API prefix — the docs,
// the document, the dashboard.
func (f *Fixture) DoRoot(t testing.TB, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.Server.Router().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// RawRequest builds a request to a root-level route with a body sent
// exactly as given. A signed webhook is verified over the bytes on the
// wire, so re-encoding them would change what is being tested.
func (f *Fixture) RawRequest(t testing.TB, method, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// Serve runs a request through the real router.
func (f *Fixture) Serve(t testing.TB, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	f.Server.Router().ServeHTTP(rec, req)
	return rec
}

func (f *Fixture) DoJSON(t testing.TB, method, path string, body any, apiKey string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := f.Do(t, method, path, body, apiKey)
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s %s response %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec
}

// RequireStatus fails the test unless rec carries the expected status,
// reporting the body — which is where the server explains itself.
func RequireStatus(t testing.TB, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("expected %d, got %d: %s", want, rec.Code, rec.Body.String())
	}
}

// HTTPServer starts a real HTTP server in front of the fixture, for a
// test that needs a client to dial it — the MCP client, notably.
func (f *Fixture) HTTPServer(t testing.TB) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(f.Server.Router())
	t.Cleanup(ts.Close)
	return ts
}

// AddDomain gives an app a name to answer at.
//
// Traefik routes by host, so an app with none cannot deploy — which
// makes this a step every deploy test needs. Port 0 means "read it from
// the image", which is what an app in a test has.
func AddDomain(t testing.TB, f *Fixture, key, ref, host string) {
	t.Helper()
	RequireStatus(t, f.Do(t, http.MethodPost, "/apps/"+ref+"/domains",
		map[string]any{"host": host}, key), http.StatusCreated)
}
