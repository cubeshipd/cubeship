// Package shell opens an interactive shell in an app's container or on
// one of the instance's machines, for somebody allowed to have one.
//
// It decides who may, and where the program runs; internal/platform/
// terminal carries the bytes. A shell on a worker runs on the worker,
// which connects back to take it — nothing dials a worker — and the
// session here only joins the two connections.
package shell

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cubeship/internal/app"
	"cubeship/internal/audit"
	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/platform/terminal"
	"cubeship/internal/user"
)

// Target is where a session runs.
type Target struct {
	// Label says where, as the terminal shows it and the log writes it.
	Label string
	// Remote is a session on a worker, NodeID which one.
	Remote bool
	NodeID int64
	// Container is the app container, or empty with Host set for the
	// machine itself.
	Container string
	Host      bool
}

// ClaimTimeout is how long a worker has to connect back once it is told
// to open a session: a poll is woken at once, so a worker that is up does
// it in well under a second, and one that has not in this long is not
// coming.
var ClaimTimeout = 15 * time.Second

var (
	// ErrNoAnswer is a worker that was told to open a session and did
	// not connect back in time.
	ErrNoAnswer = errors.New("the server did not open the shell in time — it may be busy or unreachable")
	// ErrUnknownSession is a worker claiming a session nobody is
	// waiting for, or one that is not its own.
	ErrUnknownSession = errors.New("no such session")
	// ErrPasswordRequired is a root shell opened from the dashboard
	// without the password confirming it.
	ErrPasswordRequired = errors.New("a root shell needs your password")
)

// Apps is what the shell asks of apps: which copy, for which caller.
type Apps interface {
	ShellTarget(ctx context.Context, caller *user.User, ref app.Reference, server string) (app.Replica, error)
}

// Nodes is what the shell asks of the cluster.
type Nodes interface {
	ShellHost(ctx context.Context, caller *user.User, name string) (*node.Node, error)
	ByName(ctx context.Context, name string) (*node.Node, error)
	OpenShell(nodeID int64, job node.ShellJob) error
}

// Local opens programs on this machine.
type Local interface {
	ContainerShell(ctx context.Context, containerID string, cols, rows uint16) (terminal.Process, error)
	Terminal(ctx context.Context, cols, rows uint16) (terminal.Process, error)
}

// OnThisMachine opens shells through the host runner: a container's with
// docker exec, the machine's with nsenter. See hostexec.
func OnThisMachine(r *hostexec.Runner) Local { return runner{r} }

type runner struct{ r *hostexec.Runner }

func (l runner) ContainerShell(ctx context.Context, id string, cols, rows uint16) (terminal.Process, error) {
	return l.r.ContainerShell(ctx, id, cols, rows)
}

func (l runner) Terminal(ctx context.Context, cols, rows uint16) (terminal.Process, error) {
	return l.r.Terminal(ctx, cols, rows)
}

// Passwords confirms the person at the keyboard.
type Passwords interface {
	ConfirmPassword(ctx context.Context, u *user.User, password string) error
}

// Recorder writes the audit log.
type Recorder interface {
	Record(ctx context.Context, e audit.Event)
}

// Message is what a refusal says to the person who was refused. The
// errors a module returns are written for a log; these are written for
// a terminal that is about to close.
func Message(err error) string {
	switch {
	case errors.Is(err, user.ErrUnauthenticated):
		return "you are not signed in"
	case errors.Is(err, user.ErrForbidden):
		return "you do not have permission to open this shell"
	case errors.Is(err, app.ErrNotFound), errors.Is(err, user.ErrHidden):
		return "no such app"
	case errors.Is(err, node.ErrNotFound), errors.Is(err, app.ErrNoSuchNode):
		return "no such server"
	case errors.Is(err, app.ErrNoContainer):
		return "that app has no running container to open a shell in"
	case errors.Is(err, user.ErrInvalidCredentials):
		return "wrong password"
	case errors.Is(err, dockerx.ErrNoShell), errors.Is(err, node.ErrOffline), errors.Is(err, node.ErrTooOld),
		errors.Is(err, ErrNoAnswer), errors.Is(err, ErrPasswordRequired):
		return err.Error()
	default:
		return fmt.Sprintf("could not open the shell: %v", err)
	}
}
