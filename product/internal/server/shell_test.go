package server_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/server/servertest"
	"cubeship/internal/user"

	"github.com/coder/websocket"
)

const shellPassword = "correct horse battery staple"

// fakeTerminals opens a program that says where it is and exits, and
// remembers what it was asked to open.
type fakeTerminals struct {
	mu     sync.Mutex
	opened []string
}

type said struct{ r *strings.Reader }

func (s *said) Read(b []byte) (int, error)                   { return s.r.Read(b) }
func (s *said) Write(b []byte) (int, error)                  { return len(b), nil }
func (s *said) Resize(context.Context, uint16, uint16) error { return nil }
func (s *said) Wait(context.Context) (int, error)            { return 0, nil }
func (s *said) Close() error                                 { return nil }

func (f *fakeTerminals) ContainerShell(_ context.Context, id string, _, _ uint16) (terminal.Process, error) {
	f.mu.Lock()
	f.opened = append(f.opened, "container "+id)
	f.mu.Unlock()
	return &said{strings.NewReader("in " + id)}, nil
}

func (f *fakeTerminals) Terminal(context.Context, uint16, uint16) (terminal.Process, error) {
	f.mu.Lock()
	f.opened = append(f.opened, "host")
	f.mu.Unlock()
	return &said{strings.NewReader("on the host")}, nil
}

// shellFixture is a server with an app whose container is running on
// the control plane, and terminals that never touch Docker.
func shellFixture(t *testing.T) (*servertest.Fixture, string, *fakeTerminals) {
	t.Helper()
	f := servertest.New(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPost, "/apps",
		map[string]any{"project": "web", "name": "api"}, f.AdminKey), http.StatusCreated)
	ctx := context.Background()
	repo := app.NewRepository(f.DB)
	a, err := repo.ScopedByReference(ctx, "web", "production", "api")
	if err != nil {
		t.Fatalf("find the app: %v", err)
	}
	here, err := repo.ControlPlaneID(ctx)
	if err != nil {
		t.Fatalf("find the control plane: %v", err)
	}
	if err := repo.UpdateContainer(ctx, a.ID, here, 1, "c0ffee", "api", 0, app.StatusRunning); err != nil {
		t.Fatalf("seed a running container: %v", err)
	}
	terms := &fakeTerminals{}
	f.Server.Shell.SetLocal(terms)
	return f, "ws" + strings.TrimPrefix(f.HTTPServer(t).URL, "http"), terms
}

// session opens a shell and reads it to the end: what it printed, and
// what it said instead when it refused.
func session(t *testing.T, url string, header http.Header, first *terminal.Message) (printed string, out terminal.Outcome, status int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if resp != nil {
			return "", terminal.Outcome{}, resp.StatusCode
		}
		t.Fatalf("dial %s: %v", url, err)
	}
	if first != nil {
		if err := terminal.WriteMessage(ctx, conn, *first); err != nil {
			t.Fatalf("send the first message: %v", err)
		}
	}
	var screen strings.Builder
	out = terminal.Attach(ctx, conn, emptyStdin{}, &screen, nil, nil)
	return screen.String(), out, http.StatusSwitchingProtocols
}

// emptyStdin is a keyboard nobody types on. It blocks rather than ending,
// because a closed stdin is not the same as a person who has not typed.
type emptyStdin struct{}

func (emptyStdin) Read([]byte) (int, error) { select {} }

var _ io.Reader = emptyStdin{}

func bearer(key string) http.Header { return http.Header{"Authorization": {"Bearer " + key}} }

func TestAShellOpensForWhoMayHaveOne(t *testing.T) {
	f, base, terms := shellFixture(t)
	appURL := base + "/api/apps/web/production/api/shell"

	printed, out, _ := session(t, appURL, bearer(f.AdminKey), nil)
	if out.Reason != terminal.ReasonExited || printed != "in c0ffee" {
		t.Fatalf("an admin's shell ended %+v having printed %q", out, printed)
	}

	printed, out, _ = session(t, base+"/api/nodes/control-plane/shell", bearer(f.AdminKey), nil)
	if out.Reason != terminal.ReasonExited || printed != "on the host" {
		t.Fatalf("an admin's root shell ended %+v having printed %q", out, printed)
	}
	if strings.Join(terms.opened, ",") != "container c0ffee,host" {
		t.Errorf("opened %v", terms.opened)
	}
}

// The matrix: nobody signed in never reaches the upgrade; a member is
// told no, on an app without the shell grant and on a machine always.
func TestAShellIsRefusedToWhoMayNot(t *testing.T) {
	f, base, terms := shellFixture(t)
	appURL := base + "/api/apps/web/production/api/shell"
	hostURL := base + "/api/nodes/control-plane/shell"

	if _, _, status := session(t, appURL, nil, nil); status != http.StatusUnauthorized {
		t.Errorf("nobody signed in got %d, want 401", status)
	}

	_, memberKey := f.AddMember(t, "dev", user.RoleMember)
	for _, url := range []string{appURL, hostURL} {
		_, out, _ := session(t, url, bearer(memberKey), nil)
		if out.Err == nil || out.Err.Error() != "you do not have permission to open this shell" {
			t.Errorf("a member opening %s: %+v", url, out)
		}
	}

	// A role that grants the shell on the project opens the app's, and
	// still not the machine's.
	var role struct {
		ID int64 `json:"id"`
	}
	servertest.RequireStatus(t, f.DoJSON(t, http.MethodPost, "/roles", map[string]any{
		"name": "operator",
		"grants": []user.Grant{
			{Resource: user.ResApps, Level: user.LevelManage, Shell: true},
			{Resource: user.ResServers, Level: user.LevelManage},
		},
	}, f.AdminKey, &role), http.StatusCreated)
	servertest.RequireStatus(t, f.Do(t, http.MethodPatch, "/users/dev",
		map[string]any{"access_role_id": role.ID}, f.AdminKey), http.StatusOK)

	if _, out, _ := session(t, appURL, bearer(memberKey), nil); out.Reason != terminal.ReasonExited {
		t.Errorf("the shell grant did not open the app's shell: %+v", out)
	}
	if _, out, _ := session(t, hostURL, bearer(memberKey), nil); out.Err == nil {
		t.Errorf("a role opened a root shell: %+v", out)
	}
	for _, opened := range terms.opened {
		if opened == "host" {
			t.Error("a root shell was opened for somebody refused one")
		}
	}
}

// A browser's session cookie is carried by every page on the same site,
// and an app on this instance is on the same site. So the page has to be
// the dashboard's own, and a root shell asks for the password as well.
func TestABrowserOpensAShellOnlyFromTheDashboard(t *testing.T) {
	f, base, _ := shellFixture(t)
	servertest.RequireStatus(t, f.Do(t, http.MethodPut, "/users/me/password",
		map[string]string{"new_password": shellPassword}, f.AdminKey), http.StatusOK)
	cookie := f.Login(t, "admin", shellPassword)
	host := strings.TrimPrefix(base, "ws://")

	browser := func(origin string) http.Header {
		h := http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}
		if origin != "" {
			h.Set("Origin", origin)
		}
		return h
	}
	appURL := base + "/api/apps/web/production/api/shell"
	hostURL := base + "/api/nodes/control-plane/shell"

	if _, _, status := session(t, appURL, browser("https://app.example.com"), nil); status != http.StatusForbidden {
		t.Errorf("another site's page got %d, want 403", status)
	}
	if _, _, status := session(t, appURL, browser(""), nil); status != http.StatusForbidden {
		t.Errorf("no origin at all got %d, want 403", status)
	}
	if _, out, _ := session(t, appURL, browser("http://"+host), nil); out.Reason != terminal.ReasonExited {
		t.Errorf("the dashboard's own page was refused: %+v", out)
	}

	wrong := &terminal.Message{Type: terminal.TypeAuth, Password: "not it"}
	if _, out, _ := session(t, hostURL, browser("http://"+host), wrong); out.Err == nil || out.Err.Error() != "wrong password" {
		t.Errorf("a wrong password: %+v", out)
	}
	right := &terminal.Message{Type: terminal.TypeAuth, Password: shellPassword}
	if printed, out, _ := session(t, hostURL, browser("http://"+host), right); out.Reason != terminal.ReasonExited || printed != "on the host" {
		t.Errorf("the right password: %+v, printed %q", out, printed)
	}
}

// Every session leaves two lines, and neither carries what was typed.
func TestAShellIsInTheAuditLog(t *testing.T) {
	f, base, _ := shellFixture(t)
	session(t, base+"/api/apps/web/production/api/shell", bearer(f.AdminKey), nil)

	var page struct {
		Events []struct {
			Summary string `json:"summary"`
		} `json:"events"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		servertest.RequireStatus(t, f.DoJSON(t, http.MethodGet, "/audit?target=web/production/api", nil, f.AdminKey, &page), http.StatusOK)
		if len(page.Events) >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	var actions []string
	for _, e := range page.Events {
		actions = append(actions, e.Summary)
	}
	joined := strings.Join(actions, " | ")
	if !strings.Contains(joined, "Opened a shell in web/production/api") || !strings.Contains(joined, "Closed a shell in web/production/api") {
		t.Errorf("the log says: %s", joined)
	}
}
