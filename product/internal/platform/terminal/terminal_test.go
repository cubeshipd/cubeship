package terminal_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cubeship/internal/platform/terminal"

	"github.com/coder/websocket"
)

// process is a program that prints what a test tells it to, and hands
// every keystroke it is sent to the test.
type process struct {
	out     *io.PipeReader
	print   *io.PipeWriter
	typed   chan []byte
	resized chan terminal.Size
	code    int
	closed  chan struct{}
	once    sync.Once
}

func newProcess(code int) *process {
	r, w := io.Pipe()
	return &process{out: r, print: w, typed: make(chan []byte, 16), resized: make(chan terminal.Size, 4),
		code: code, closed: make(chan struct{})}
}

func (p *process) Read(b []byte) (int, error) { return p.out.Read(b) }
func (p *process) Write(b []byte) (int, error) {
	p.typed <- append([]byte(nil), b...)
	return len(b), nil
}

func (p *process) Resize(_ context.Context, cols, rows uint16) error {
	p.resized <- terminal.Size{Cols: cols, Rows: rows}
	return nil
}
func (p *process) Wait(context.Context) (int, error) { return p.code, nil }
func (p *process) Close() error {
	p.once.Do(func() {
		close(p.closed)
		p.print.Close()
	})
	return nil
}

// serve runs p behind a WebSocket and reports how the session ended.
func serve(t *testing.T, p terminal.Process) (string, <-chan terminal.Outcome) {
	t.Helper()
	ended := make(chan terminal.Outcome, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ended <- terminal.Serve(r.Context(), conn, p, "web/production/api on control-plane")
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http"), ended
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// syncBuffer is what a client prints to, read from another goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func receive[T any](t *testing.T, what string, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// The whole conversation: keys reach the program, what it prints reaches
// the screen, a resize reaches it as a resize rather than as bytes, and
// its exit status reaches the client.
func TestASessionCarriesBothWaysAndEndsWithTheProgram(t *testing.T) {
	p := newProcess(3)
	url, ended := serve(t, p)

	keys, typing := io.Pipe()
	defer typing.Close()
	screen := &syncBuffer{}
	resizes := make(chan terminal.Size, 1)
	landed := make(chan string, 1)
	attached := make(chan terminal.Outcome, 1)
	go func() {
		attached <- terminal.Attach(context.Background(), dial(t, url), keys, screen, resizes,
			func(target string) { landed <- target })
	}()

	if got := receive(t, "the ready frame", landed); got != "web/production/api on control-plane" {
		t.Errorf("landed on %q", got)
	}

	typing.Write([]byte("ls\n"))
	if got := receive(t, "a keystroke", p.typed); string(got) != "ls\n" {
		t.Errorf("the program was typed %q, want ls\\n", got)
	}

	resizes <- terminal.Size{Cols: 120, Rows: 40}
	if got := receive(t, "a resize", p.resized); got != (terminal.Size{Cols: 120, Rows: 40}) {
		t.Errorf("resized to %+v", got)
	}

	p.print.Write([]byte("hello\r\n"))
	eventually(t, "output on the screen", func() bool { return screen.String() == "hello\r\n" })

	p.print.Close()
	out := receive(t, "the client's outcome", attached)
	if out.Reason != terminal.ReasonExited || out.Code != 3 {
		t.Errorf("the client saw %+v, want exited 3", out)
	}
	if got := receive(t, "the server's outcome", ended); got.Reason != terminal.ReasonExited || got.Code != 3 {
		t.Errorf("the server saw %+v, want exited 3", got)
	}
}

// Closing the terminal is ending the program — a shell left running
// inside somebody's container after its window closed is a process
// nobody can see.
func TestClosingTheClientClosesTheProgram(t *testing.T) {
	p := newProcess(0)
	url, ended := serve(t, p)

	conn := dial(t, url)
	if _, _, err := conn.Read(context.Background()); err != nil {
		t.Fatalf("read the ready frame: %v", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")

	receive(t, "the program to be closed", p.closed)
	if got := receive(t, "the server's outcome", ended); got.Reason != terminal.ReasonClosed {
		t.Errorf("ended %+v, want closed", got)
	}
}

func TestAnIdleSessionIsClosed(t *testing.T) {
	defer func(d time.Duration) { terminal.IdleTimeout = d }(terminal.IdleTimeout)
	terminal.IdleTimeout = 100 * time.Millisecond

	p := newProcess(0)
	url, ended := serve(t, p)
	keys, typing := io.Pipe()
	defer typing.Close()

	out := terminal.Attach(context.Background(), dial(t, url), keys, io.Discard, nil, nil)
	if out.Reason != terminal.ReasonFailed || out.Err == nil || out.Err.Error() != terminal.ErrIdle.Error() {
		t.Errorf("the client saw %+v, want the idle refusal", out)
	}
	if got := receive(t, "the server's outcome", ended); got.Reason != terminal.ReasonIdle {
		t.Errorf("ended %+v, want idle", got)
	}
	receive(t, "the program to be closed", p.closed)
}

// The control plane between a browser and a worker copies frames it does
// not understand, and still learns how the session ended.
func TestARelayCarriesASessionUnchanged(t *testing.T) {
	p := newProcess(7)
	worker, _ := serve(t, p)

	relayed := make(chan terminal.Outcome, 1)
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		relayed <- terminal.Relay(r.Context(), client, dial(t, worker))
	}))
	defer relay.Close()

	keys, typing := io.Pipe()
	defer typing.Close()
	screen := &syncBuffer{}
	attached := make(chan terminal.Outcome, 1)
	go func() {
		attached <- terminal.Attach(context.Background(), dial(t, "ws"+strings.TrimPrefix(relay.URL, "http")),
			keys, screen, nil, nil)
	}()

	typing.Write([]byte("whoami\n"))
	if got := receive(t, "a keystroke through the relay", p.typed); string(got) != "whoami\n" {
		t.Errorf("the program was typed %q", got)
	}
	p.print.Write([]byte("root\r\n"))
	eventually(t, "output through the relay", func() bool { return screen.String() == "root\r\n" })
	p.print.Close()

	if out := receive(t, "the client's outcome", attached); out.Reason != terminal.ReasonExited || out.Code != 7 {
		t.Errorf("the client saw %+v, want exited 7", out)
	}
	if out := receive(t, "the relay's outcome", relayed); out.Reason != terminal.ReasonExited || out.Code != 7 {
		t.Errorf("the relay saw %+v, want exited 7", out)
	}
}
