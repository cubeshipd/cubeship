// Package terminal carries an interactive program over a WebSocket.
//
// It knows nothing about what the program is — a shell in an app's
// container, a root shell on a machine — or who asked for it. Those are
// decided above it; this is the wire, used the same way by the control
// plane serving a browser, by a worker serving the control plane, and by
// the CLI on the other end.
//
// **The protocol is two kinds of frame.** A binary frame is terminal
// bytes, in either direction, exactly as the program wrote them or as the
// keyboard produced them. A text frame is a Message: the one thing a byte
// stream cannot say, which is that the window changed size, the program
// ended, or something went wrong. Keeping them in separate frame types
// rather than escaping one inside the other means no byte a program can
// print is ever mistaken for an instruction.
package terminal

import (
	"context"
	"errors"
	"io"
	"time"
)

// Size is a terminal's size in cells.
type Size struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// DefaultSize is what a session starts at when the client says nothing.
// The size of a terminal nobody resized, which is what every program
// already expects when it cannot find out.
var DefaultSize = Size{Cols: 80, Rows: 24}

// Valid reports a size a program can be given. Zero in either is a
// window that has not been laid out yet, and passing it on makes a
// full-screen program draw into nothing.
func (s Size) Valid() bool { return s.Cols > 0 && s.Rows > 0 }

// Process is a program attached to a terminal.
//
// Reading is what it prints; writing is what it is typed. Wait is asked
// once reading has ended, and Close ends it early — it must be safe to
// call after the program has exited on its own, and more than once.
type Process interface {
	io.Reader
	io.Writer
	Resize(ctx context.Context, cols, rows uint16) error
	Wait(ctx context.Context) (int, error)
	Close() error
}

// Message types.
const (
	// TypeResize is the client's window changing size.
	TypeResize = "resize"
	// TypeAuth is the password a session asks for before it starts, for
	// the one kind that does. See Password.
	TypeAuth = "auth"
	// TypeReady says the program is running and bytes may flow. A client
	// that shows a spinner until the first byte would spin forever on a
	// shell whose prompt is empty.
	TypeReady = "ready"
	// TypeExit is the program ending, with its status.
	TypeExit = "exit"
	// TypeError is the session ending for a reason that is not the
	// program's: it could not start, it sat idle, the machine went away.
	TypeError = "error"
)

// Message is a text frame.
type Message struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// Code is set on exit. A pointer, because 0 is the most common
	// status there is and must not read as "not said".
	Code *int `json:"code,omitempty"`
	// Message is what went wrong, in a sentence somebody reads.
	Message string `json:"message,omitempty"`
	// Password travels once, in the first frame, and never in a URL: a
	// query string ends up in every access log between here and the
	// browser.
	Password string `json:"password,omitempty"`
	// Target says what the session reached — which machine, which copy
	// — so a terminal can say where it is.
	Target string `json:"target,omitempty"`
}

// Timing. Package variables rather than constants so a test can make a
// session go idle in milliseconds instead of half an hour.
var (
	// IdleTimeout closes a session nothing has happened on — no key
	// pressed and nothing printed. Both, deliberately: a `tail -f`
	// nobody is typing into is not idle, and a shell somebody walked away
	// from is.
	IdleTimeout = 30 * time.Minute
	// PingInterval keeps every proxy between here and the browser from
	// deciding a quiet connection is a dead one. Traefik's idle timeout
	// is three minutes.
	PingInterval = 30 * time.Second
	// WaitTimeout bounds asking a program's status after its output has
	// ended. The Engine answers at once almost always; a daemon that does
	// not must not hold a session open.
	WaitTimeout = 5 * time.Second
)

// MaxFrame is the largest frame a session reads. A paste is one frame, and
// the library's default of 32 KiB cuts a pasted file in half with an
// error nobody can act on.
const MaxFrame = 1 << 20

// Reasons a session ended, as the audit log writes them.
const (
	ReasonExited  = "exited"
	ReasonClosed  = "closed"
	ReasonIdle    = "idle"
	ReasonFailed  = "failed"
	ReasonStopped = "stopped"
)

// ErrIdle is a session closed for sitting idle.
var ErrIdle = errors.New("closed after 30 minutes without activity")

// Outcome is how a session ended.
type Outcome struct {
	Reason string
	// Code is the program's exit status, when it exited.
	Code int
	// Err is what went wrong, when something did.
	Err error
}
