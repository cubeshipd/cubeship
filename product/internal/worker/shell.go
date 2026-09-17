package worker

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"cubeship/internal/node"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/terminal"

	"github.com/coder/websocket"
)

// terminals is what opens a shell on this machine. The host runner is
// one; asserted rather than required, the way the component inventory's
// Engine is, so an agent built without it says it cannot rather than
// failing to start.
type terminals interface {
	ContainerShell(ctx context.Context, containerID string, cols, rows uint16) (*dockerx.TTY, error)
	Terminal(ctx context.Context, cols, rows uint16) (*dockerx.TTY, error)
}

// shellDialTimeout bounds connecting back. The control plane gives up
// on a session it is holding after fifteen seconds, and a connection
// that lands after that has nobody to join.
const shellDialTimeout = 15 * time.Second

// shell opens a session the control plane asked for and connects it
// back.
//
// Connected first and opened second, so whatever goes wrong opening it
// — an image with no shell, a container that just stopped — reaches the
// person as a sentence rather than as a session that never arrived.
func (a *Agent) shell(cmd node.Command) {
	job := cmd.Shell
	if job == nil {
		return
	}
	ctx := context.Background()

	dial, cancel := context.WithTimeout(ctx, shellDialTimeout)
	defer cancel()
	conn, _, err := websocket.Dial(dial, wsURL(a.controlPlane)+"/api/nodes/agent/shell/"+job.Session, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + a.token}},
	})
	if err != nil {
		log.Printf("agent: connecting a shell back: %v", err)
		return
	}

	t, ok := a.firewall.(terminals)
	if !ok || t == nil {
		terminal.Fail(ctx, conn, errors.New("this server's daemon cannot open a shell"))
		return
	}
	var tty *dockerx.TTY
	if job.Host {
		tty, err = t.Terminal(ctx, job.Cols, job.Rows)
	} else {
		tty, err = t.ContainerShell(ctx, job.Container, job.Cols, job.Rows)
	}
	if err != nil {
		terminal.Fail(ctx, conn, err)
		return
	}
	terminal.Serve(ctx, conn, tty, job.Target)
}

// wsURL is the control plane's address with the scheme a WebSocket
// dials.
func wsURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base
}
