package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"cubeship/internal/platform/terminal"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// exitError carries a remote shell's exit status out through cobra, so
// `cubeship app shell web/api` exits with what the shell exited with and
// a script can tell a failed command from a successful one.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// attachShell runs a session on this terminal until it ends.
//
// The local terminal goes raw for the duration: every key, Ctrl-C
// included, is the remote shell's to interpret, which is what makes
// interrupting a command over there interrupt that command rather than
// this CLI. Restored however the session ends.
func attachShell(ctx context.Context, open func(terminal.Size) (*websocket.Conn, error)) error {
	fd := int(os.Stdin.Fd())
	interactive := term.IsTerminal(fd)
	size := currentSize()

	conn, err := open(size)
	if err != nil {
		return err
	}

	if interactive {
		state, err := term.MakeRaw(fd)
		if err != nil {
			conn.CloseNow()
			return fmt.Errorf("put the terminal in raw mode: %w", err)
		}
		defer term.Restore(fd, state)
	}

	resizes := make(chan terminal.Size, 1)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			select {
			case resizes <- currentSize():
			default:
			}
		}
	}()

	out := terminal.Attach(ctx, conn, os.Stdin, os.Stdout, resizes, nil)
	if interactive {
		// Raw mode does not translate a newline, so the line after the
		// session would start where the prompt left off.
		fmt.Fprint(os.Stdout, "\r\n")
	}
	switch out.Reason {
	case terminal.ReasonExited:
		if out.Code != 0 {
			return exitError{code: out.Code}
		}
		return nil
	case terminal.ReasonStopped:
		return nil
	}
	if out.Err != nil {
		return out.Err
	}
	return errors.New("the shell closed")
}

func currentSize() terminal.Size {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		return terminal.DefaultSize
	}
	return terminal.Size{Cols: uint16(cols), Rows: uint16(rows)}
}

// newAppShellCmd is `cubeship app shell`.
func newAppShellCmd() *cobra.Command {
	var server string
	cmd := &cobra.Command{
		Use:   "shell <app>",
		Short: "Open a shell inside an app's running container",
		Long: "Open an interactive shell inside an app's running container.\n\n" +
			"It runs bash where the image has it and sh otherwise, as the\n" +
			"container's own user and with the app's environment. An image\n" +
			"with no shell at all — distroless, or built from scratch — is\n" +
			"refused. Exiting the shell ends the session, and the command\n" +
			"exits with the shell's status.\n\n" +
			"An app running on several servers opens on the first running\n" +
			"copy; --server picks one.\n\n" +
			"Opening a shell needs the shell permission on the app's project,\n" +
			"which an admin has and a role has to grant explicitly. Every\n" +
			"session is in the audit log; what is typed into it is not.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return attachShell(cmd.Context(), func(size terminal.Size) (*websocket.Conn, error) {
				return c.AppShell(cmd.Context(), args[0], server, size)
			})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "the server whose copy to open it in (default: the first running copy)")
	return cmd
}

// newServerShellCmd is `cubeship server shell`.
func newServerShellCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell <name>",
		Short: "Open a root shell on a machine",
		Long: "Open an interactive root shell on one of this instance's machines —\n" +
			"the same as signing in to it over SSH as root, without an SSH port.\n\n" +
			"Only an admin can, with a key that is not restricted. The shell\n" +
			"runs on the machine itself: its files, its services, its Docker.\n" +
			"A worker connects the session back to the control plane, so it\n" +
			"needs no port open either. The session is in the audit log; what\n" +
			"is typed into it is not.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newAPIClient()
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return attachShell(cmd.Context(), func(size terminal.Size) (*websocket.Conn, error) {
				return c.ServerShell(cmd.Context(), args[0], size)
			})
		},
	}
}
