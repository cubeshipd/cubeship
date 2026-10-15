package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/node"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/user"

	"github.com/coder/websocket"
)

// Service opens sessions.
type Service struct {
	apps      Apps
	nodes     Nodes
	local     Local
	passwords Passwords

	mu sync.Mutex
	// claims are sessions waiting for a worker to connect back, by id.
	// In memory, like every command in flight: a session means nothing
	// once the connection that asked for it is gone.
	claims map[string]*claim
}

type claim struct {
	node int64
	conn chan *websocket.Conn
	// done releases the worker's request once the session is over. Its
	// handler has to stay running for as long as the connection it
	// accepted is in use.
	done chan struct{}
}

func NewService(apps Apps, nodes Nodes, local Local, passwords Passwords) *Service {
	return &Service{apps: apps, nodes: nodes, local: local, passwords: passwords, claims: map[string]*claim{}}
}

// SetLocal replaces what opens programs on this machine. For a test,
// which has no Docker to open one with.
func (s *Service) SetLocal(l Local) { s.local = l }

// AppTarget is where a shell in an app opens, for caller.
func (s *Service) AppTarget(ctx context.Context, caller *user.User, ref app.Reference, server string) (Target, error) {
	r, err := s.apps.ShellTarget(ctx, caller, ref, server)
	if err != nil {
		return Target{}, err
	}
	t := Target{Label: ref.String() + " on " + r.NodeSlug, Container: r.Container}
	if r.NodeSlug == node.ControlPlaneSlug {
		return t, nil
	}
	n, err := s.nodes.ByName(ctx, r.NodeSlug)
	if err != nil {
		return Target{}, err
	}
	if err := n.CanOpenShell(); err != nil {
		return Target{}, err
	}
	t.Remote, t.NodeID = true, n.ID
	return t, nil
}

// HostTarget is a root shell on a machine, for caller.
func (s *Service) HostTarget(ctx context.Context, caller *user.User, name string) (Target, error) {
	n, err := s.nodes.ShellHost(ctx, caller, name)
	if err != nil {
		return Target{}, err
	}
	return Target{Label: "root on " + n.Slug, Host: true, Remote: !n.ControlPlane, NodeID: n.ID}, nil
}

// ConfirmPassword is the step a root shell from the dashboard takes
// before it starts.
func (s *Service) ConfirmPassword(ctx context.Context, caller *user.User, password string) error {
	if password == "" {
		return ErrPasswordRequired
	}
	return s.passwords.ConfirmPassword(ctx, caller, password)
}

// Run is the session: it opens the program where the target says and
// joins it to conn until one of them ends.
func (s *Service) Run(ctx context.Context, conn *websocket.Conn, t Target, size terminal.Size) terminal.Outcome {
	if t.Remote {
		return s.remote(ctx, conn, t, size)
	}
	var p terminal.Process
	var err error
	if t.Host {
		p, err = s.local.Terminal(ctx, size.Cols, size.Rows)
	} else {
		p, err = s.local.ContainerShell(ctx, t.Container, size.Cols, size.Rows)
	}
	if err != nil {
		terminal.Fail(ctx, conn, fmt.Errorf("%s", Message(err)))
		return terminal.Outcome{Reason: terminal.ReasonFailed, Err: err}
	}
	return terminal.Serve(ctx, conn, p, t.Label)
}

// remote asks the worker to open the program and connect back, and
// joins the two connections once it has.
func (s *Service) remote(ctx context.Context, conn *websocket.Conn, t Target, size terminal.Size) terminal.Outcome {
	id, err := sessionID()
	if err != nil {
		terminal.Fail(ctx, conn, err)
		return terminal.Outcome{Reason: terminal.ReasonFailed, Err: err}
	}
	c := &claim{node: t.NodeID, conn: make(chan *websocket.Conn, 1), done: make(chan struct{})}
	s.mu.Lock()
	s.claims[id] = c
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.claims, id)
		s.mu.Unlock()
		close(c.done)
	}()

	job := node.ShellJob{Session: id, Container: t.Container, Host: t.Host, Cols: size.Cols, Rows: size.Rows, Target: t.Label}
	if err := s.nodes.OpenShell(t.NodeID, job); err != nil {
		terminal.Fail(ctx, conn, err)
		return terminal.Outcome{Reason: terminal.ReasonFailed, Err: err}
	}

	timer := time.NewTimer(ClaimTimeout)
	defer timer.Stop()
	select {
	case worker := <-c.conn:
		return terminal.Relay(ctx, conn, worker)
	case <-timer.C:
		terminal.Fail(ctx, conn, ErrNoAnswer)
		return terminal.Outcome{Reason: terminal.ReasonFailed, Err: ErrNoAnswer}
	case <-ctx.Done():
		conn.CloseNow()
		return terminal.Outcome{Reason: terminal.ReasonStopped}
	}
}

// Claim hands a worker's connection to the session waiting for it, and
// holds until that session is over. A session is claimed once, and only
// by the machine it was sent to: without that, any worker's credential
// would be a way onto somebody else's screen.
func (s *Service) Claim(ctx context.Context, from *node.Node, id string, conn *websocket.Conn) error {
	s.mu.Lock()
	c, ok := s.claims[id]
	if ok && c.node == from.ID {
		delete(s.claims, id)
	}
	s.mu.Unlock()
	if !ok || c.node != from.ID {
		return ErrUnknownSession
	}
	select {
	case c.conn <- conn:
	default:
		return ErrUnknownSession
	}
	select {
	case <-c.done:
	case <-ctx.Done():
	}
	return nil
}

func sessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
