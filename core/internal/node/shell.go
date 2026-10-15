package node

import (
	"context"
	"errors"
	"net/http"

	"cubeship/internal/user"

	"github.com/Masterminds/semver/v3"
)

// CommandShell tells a machine to open a terminal and dial it back to
// the control plane. See internal/shell: nothing dials a worker, so the
// worker is what connects, and the command is how it learns to.
const CommandShell = "shell"

// ShellJob is what a machine needs to open a session: where to connect
// it back, and what to run on it.
type ShellJob struct {
	// Session names the connection the control plane is holding for
	// this machine. It is single-use and short-lived, and only this
	// machine's credential may claim it.
	Session string `json:"session"`
	// Container is the app container to open a shell in. Empty with
	// Host set means the machine itself.
	Container string `json:"container,omitempty"`
	Host      bool   `json:"host,omitempty"`
	Cols      uint16 `json:"cols"`
	Rows      uint16 `json:"rows"`
	// Target is where the session is, as the terminal shows it.
	Target string `json:"target,omitempty"`
}

// ShellSince is the first release whose agent opens a shell. A machine
// on an older one would be sent a command it answers with "I do not know
// how", after the person had watched a spinner for fifteen seconds.
var ShellSince = semver.MustParse("0.10.0")

var (
	// ErrOffline is a machine that has stopped calling in. Nothing can
	// reach one, so asking it to open a shell would only time out.
	ErrOffline = errors.New("that server is not calling in, so nothing can open a shell on it")
	// ErrTooOld is a machine whose daemon predates shells.
	ErrTooOld = errors.New("that server runs a version of Cubeship without shells — update it first")
)

// CanOpenShell reports why a shell cannot be opened on n, or nil.
//
// A version that is not a release — `make dev`, a build from a branch —
// is assumed to be current. It is somebody working on Cubeship itself,
// and refusing them over a version string would be refusing the one
// person who knows what they are running.
func (n *Node) CanOpenShell() error {
	if n.ControlPlane {
		return nil
	}
	if n.Status() != StatusReady {
		return ErrOffline
	}
	v, err := semver.NewVersion(n.Version)
	if err != nil {
		return nil
	}
	// Prereleases of the version that introduced shells have them too;
	// semver orders 0.10.0-rc.1 below 0.10.0, so compare without it.
	core, _ := v.SetPrerelease("")
	if core.LessThan(ShellSince) {
		return ErrTooOld
	}
	return nil
}

// ShellHost is the machine a root shell opens on, for a caller allowed
// one. Only an admin, and only one whose key is not restricted: a root
// prompt on the host is everything, so there is no grant that reaches it.
func (s *Service) ShellHost(ctx context.Context, caller *user.User, name string) (*Node, error) {
	if caller == nil {
		return nil, user.ErrUnauthenticated
	}
	if !caller.Admin() {
		return nil, user.ErrForbidden
	}
	n, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	if err := n.CanOpenShell(); err != nil {
		return nil, err
	}
	return n, nil
}

// ByName is a machine by its name, for a module that already decided
// the caller may reach what is on it.
func (s *Service) ByName(ctx context.Context, name string) (*Node, error) {
	n, err := s.Repo().BySlug(ctx, name)
	if err != nil {
		return nil, ErrNotFound
	}
	return n, nil
}

// OpenShell tells a machine to open a session and connect it back.
// Nothing comes back on the command channel: the answer is the machine
// connecting, or not.
func (s *Service) OpenShell(nodeID int64, job ShellJob) error {
	return s.hub.Tell(nodeID, Command{Kind: CommandShell, Shell: &job})
}

// Agent authenticates a worker's own credential, for a module that
// mounts a route only machines call. See agent.
func (h *Handler) Agent(next http.Handler) http.Handler { return h.agent(next) }

// AgentFromContext is the machine an Agent-wrapped request came from.
func AgentFromContext(ctx context.Context) *Node {
	n, _ := ctx.Value(nodeContextKey{}).(*Node)
	return n
}
