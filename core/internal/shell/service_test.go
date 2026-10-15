package shell

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/user"

	"github.com/coder/websocket"
)

type apps struct{ replica app.Replica }

func (a apps) ShellTarget(context.Context, *user.User, app.Reference, string) (app.Replica, error) {
	return a.replica, nil
}

type nodes struct {
	worker *node.Node
	opened chan node.ShellJob
}

func (n nodes) ShellHost(context.Context, *user.User, string) (*node.Node, error) {
	return n.worker, nil
}
func (n nodes) ByName(context.Context, string) (*node.Node, error) { return n.worker, nil }
func (n nodes) OpenShell(_ int64, job node.ShellJob) error {
	n.opened <- job
	return nil
}

type noLocal struct{}

func (noLocal) ContainerShell(context.Context, string, uint16, uint16) (terminal.Process, error) {
	return nil, errors.New("not on this machine")
}
func (noLocal) Terminal(context.Context, uint16, uint16) (terminal.Process, error) {
	return nil, errors.New("not on this machine")
}

// exits is a program that prints one line and exits with a status.
type exits struct {
	r *strings.Reader
}

func (e *exits) Read(b []byte) (int, error)                   { return e.r.Read(b) }
func (e *exits) Write(b []byte) (int, error)                  { return len(b), nil }
func (e *exits) Resize(context.Context, uint16, uint16) error { return nil }
func (e *exits) Wait(context.Context) (int, error)            { return 4, nil }
func (e *exits) Close() error                                 { return nil }

func worker(t *testing.T) *node.Node {
	now := time.Now()
	return &node.Node{ID: 7, Slug: "worker-1", Version: "0.10.0", LastSeenAt: &now}
}

// client runs a session through svc the way the HTTP handler does, and
// returns the socket a person's terminal would hold.
func client(t *testing.T, svc *Service, target Target) (*websocket.Conn, <-chan terminal.Outcome) {
	t.Helper()
	ended := make(chan terminal.Outcome, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ended <- svc.Run(context.Background(), conn, target, terminal.DefaultSize)
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn, ended
}

// programConn is the worker's end: a session served to whoever dials it,
// which here is the test standing in for the control plane accepting the
// worker's connection.
func programConn(t *testing.T, p terminal.Process) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		terminal.Serve(context.Background(), conn, p, "web/production/api on worker-1")
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func TestAShellOnAWorkerIsClaimedByThatWorkerOnce(t *testing.T) {
	w := worker(t)
	opened := make(chan node.ShellJob, 1)
	svc := NewService(apps{app.Replica{NodeSlug: "worker-1", Container: "abc"}}, nodes{worker: w, opened: opened}, noLocal{}, nil)

	target, err := svc.AppTarget(context.Background(), &user.User{Role: user.RoleAdmin}, app.Reference{Project: "web", Environment: "production", Name: "api"}, "")
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if !target.Remote || target.NodeID != w.ID || target.Container != "abc" {
		t.Fatalf("target is %+v, want the container on worker-1", target)
	}

	conn, ended := client(t, svc, target)
	var job node.ShellJob
	select {
	case job = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker was never told to open the shell")
	}
	if job.Container != "abc" || job.Host || job.Session == "" {
		t.Errorf("the worker was told %+v", job)
	}

	// Another machine's credential cannot take it.
	other := &node.Node{ID: 8}
	if err := svc.Claim(context.Background(), other, job.Session, nil); !errors.Is(err, ErrUnknownSession) {
		t.Errorf("another machine claimed the session: %v", err)
	}

	claimed := make(chan error, 1)
	go func() {
		claimed <- svc.Claim(context.Background(), w, job.Session, programConn(t, &exits{r: strings.NewReader("hi\r\n")}))
	}()

	out := terminal.Attach(context.Background(), conn, strings.NewReader(""), io.Discard, nil, nil)
	if out.Reason != terminal.ReasonExited || out.Code != 4 {
		t.Errorf("the client saw %+v, want exited 4", out)
	}
	if got := <-ended; got.Reason != terminal.ReasonExited || got.Code != 4 {
		t.Errorf("the session ended %+v, want exited 4", got)
	}
	if err := <-claimed; err != nil {
		t.Errorf("the worker's claim: %v", err)
	}

	// And it was claimed once.
	if err := svc.Claim(context.Background(), w, job.Session, nil); !errors.Is(err, ErrUnknownSession) {
		t.Errorf("a session was claimed twice: %v", err)
	}
}

func TestAWorkerThatNeverConnectsBackEndsTheSessionWithAReason(t *testing.T) {
	defer func(d time.Duration) { ClaimTimeout = d }(ClaimTimeout)
	ClaimTimeout = 100 * time.Millisecond

	opened := make(chan node.ShellJob, 1)
	svc := NewService(apps{}, nodes{worker: worker(t), opened: opened}, noLocal{}, nil)
	conn, ended := client(t, svc, Target{Remote: true, NodeID: 7, Host: true})

	out := terminal.Attach(context.Background(), conn, strings.NewReader(""), io.Discard, nil, nil)
	if out.Err == nil || out.Err.Error() != ErrNoAnswer.Error() {
		t.Errorf("the client saw %+v, want %v", out, ErrNoAnswer)
	}
	if got := <-ended; !errors.Is(got.Err, ErrNoAnswer) {
		t.Errorf("the session ended %+v", got)
	}
}

func TestARefusalReadsAsASentence(t *testing.T) {
	for err, want := range map[error]string{
		user.ErrForbidden:          "you do not have permission to open this shell",
		dockerx.ErrNoShell:         dockerx.ErrNoShell.Error(),
		node.ErrTooOld:             node.ErrTooOld.Error(),
		app.ErrNoContainer:         "that app has no running container to open a shell in",
		user.ErrInvalidCredentials: "wrong password",
	} {
		if got := Message(err); got != want {
			t.Errorf("Message(%v) = %q, want %q", err, got, want)
		}
	}
}
