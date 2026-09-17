package dockerx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
)

// ErrNoShell is a container whose image has no shell to run: distroless,
// or built FROM scratch. It is refused before anything starts, so the
// person gets a sentence rather than a terminal that opens and closes.
var ErrNoShell = errors.New("this app's image has no shell (/bin/sh) — it may be distroless or built from scratch")

// TTY is a program attached to a terminal the Engine allocated: an exec
// inside a running container, or a container of its own.
//
// Read and Write are the terminal's bytes, unframed. With a TTY the
// Engine has no second stream to multiplex, so what it sends is exactly
// what the program printed.
type TTY struct {
	stream types.HijackedResponse
	resize func(ctx context.Context, cols, rows uint16) error
	wait   func(ctx context.Context) (int, error)
	stop   func()
	once   sync.Once
}

func (t *TTY) Read(p []byte) (int, error)  { return t.stream.Reader.Read(p) }
func (t *TTY) Write(p []byte) (int, error) { return t.stream.Conn.Write(p) }

func (t *TTY) Resize(ctx context.Context, cols, rows uint16) error { return t.resize(ctx, cols, rows) }

func (t *TTY) Wait(ctx context.Context) (int, error) { return t.wait(ctx) }

// Close ends the program. Safe to call after it has exited, and twice.
func (t *TTY) Close() error {
	t.once.Do(func() {
		_ = t.stream.CloseWrite()
		t.stream.Close()
		if t.stop != nil {
			go t.stop()
		}
	})
	return nil
}

// Hangup is how an exec that outlived its terminal is ended: given the
// process id the Engine reports — the host's — it signals it from
// outside the container. Nil leaves such a process running, which is
// what `make dev` does, having no way onto the host.
type Hangup func(ctx context.Context, pid int, signal string) error

// ExecTTY starts cmd inside a running container on a terminal of the
// given size.
//
// **Closing the terminal is not the process ending.** The Engine closes
// the exec's stdin when the connection goes, and an interactive shell
// reads that as logout — but a program running in the foreground does
// not read stdin at all, and would go on for ever inside somebody's app.
// So an exec still running a moment after its terminal closed is sent
// SIGHUP, which is what a terminal closing has meant since there were
// terminals, and SIGKILL if that did not do it.
func (c *Client) ExecTTY(ctx context.Context, containerID string, cmd, env []string, cols, rows uint16, hangup Hangup) (*TTY, error) {
	size := &[2]uint{uint(rows), uint(cols)}
	created, err := c.api.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          cmd,
		Env:          env,
		Tty:          true,
		ConsoleSize:  size,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create exec: %w", err)
	}
	stream, err := c.api.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{Tty: true, ConsoleSize: size})
	if err != nil {
		return nil, fmt.Errorf("attach exec: %w", err)
	}

	api := c.api
	return &TTY{
		stream: stream,
		resize: func(ctx context.Context, cols, rows uint16) error {
			return api.ContainerExecResize(ctx, created.ID, container.ResizeOptions{Width: uint(cols), Height: uint(rows)})
		},
		wait: func(ctx context.Context) (int, error) {
			// The stream ending and the Engine recording the exit are
			// not one event, so the first inspect can still say
			// running. Asked again briefly rather than answered wrong.
			for {
				info, err := api.ContainerExecInspect(ctx, created.ID)
				if err != nil {
					return 0, fmt.Errorf("inspect exec: %w", err)
				}
				if !info.Running {
					return info.ExitCode, nil
				}
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-time.After(50 * time.Millisecond):
				}
			}
		},
		stop: func() {
			if hangup == nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, signal := range []string{"HUP", "KILL"} {
				time.Sleep(2 * time.Second)
				info, err := api.ContainerExecInspect(ctx, created.ID)
				if err != nil || !info.Running || info.Pid <= 0 {
					return
				}
				_ = hangup(ctx, info.Pid, signal)
			}
		},
	}, nil
}

// HasPath reports whether path exists inside a container. For a shell,
// asked before one is started: an exec of a program that is not there
// fails after the terminal is already open, and says so in the Engine's
// words.
func (c *Client) HasPath(ctx context.Context, containerID, path string) (bool, error) {
	if _, err := c.api.ContainerStatPath(ctx, containerID, path); err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("look for %s in the container: %w", path, err)
	}
	return true, nil
}

// ShellCommand is what an interactive shell in somebody's container is
// started as: bash where the image has it, sh otherwise. Decided inside
// the container by sh itself, because which of the two exists is only
// known there.
var ShellCommand = []string{"/bin/sh", "-c", "if [ -x /bin/bash ]; then exec /bin/bash; fi; exec /bin/sh"}

// ShellEnv is what a shell needs that a container's own environment
// never sets, because nothing in it expected a terminal.
var ShellEnv = []string{"TERM=xterm-256color"}

// ContainerTTY creates a container on a terminal, attaches to it and
// starts it. The container goes when the session does: its stdin closes
// with the connection, and it is removed on Close whichever ended first.
//
// Attached before it is started, so nothing the program prints first —
// a login banner, a prompt — is written to a terminal nobody is reading.
func (c *Client) ContainerTTY(ctx context.Context, opts ContainerOpts, cols, rows uint16) (*TTY, error) {
	opts.TTY = true
	opts.ConsoleSize = [2]uint{uint(rows), uint(cols)}
	id, err := c.CreateContainer(ctx, opts)
	if err != nil {
		return nil, err
	}
	remove := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.RemoveContainer(ctx, id)
	}

	stream, err := c.api.ContainerAttach(ctx, id, container.AttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true})
	if err != nil {
		remove()
		return nil, fmt.Errorf("attach container: %w", err)
	}
	// Asked for before the start, so an exit that happens at once is not
	// missed between the two.
	waited, waitErr := c.api.ContainerWait(context.WithoutCancel(ctx), id, container.WaitConditionNextExit)
	if err := c.StartContainer(ctx, id); err != nil {
		stream.Close()
		remove()
		return nil, err
	}

	api := c.api
	return &TTY{
		stream: stream,
		resize: func(ctx context.Context, cols, rows uint16) error {
			return api.ContainerResize(ctx, id, container.ResizeOptions{Width: uint(cols), Height: uint(rows)})
		},
		wait: func(ctx context.Context) (int, error) {
			select {
			case res := <-waited:
				if res.Error != nil {
					return 0, errors.New(res.Error.Message)
				}
				return int(res.StatusCode), nil
			case err := <-waitErr:
				return 0, fmt.Errorf("wait for container: %w", err)
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		},
		stop: remove,
	}, nil
}

// SignalCommand is argv for sending signal to pid from the host.
func SignalCommand(pid int, signal string) []string {
	return []string{"kill", "-" + signal, strconv.Itoa(pid)}
}
