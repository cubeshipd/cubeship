package hostexec

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"cubeship/internal/platform/dockerx"
)

// ShellLabel marks a container that is somebody's root shell on this
// machine, so one a crashed daemon left behind can be found and removed.
const ShellLabel = "cubeship.shell"

// hostShell is what runs once nsenter is in the host's namespaces.
//
// The environment is the container's, not the host's — nsenter changes
// namespaces, not variables — so HOME and PATH are set to what a root
// login would have before a login shell reads /etc/profile for the
// rest. bash where the host has it, sh otherwise.
const hostShell = `cd /root 2>/dev/null || cd /
if [ -x /bin/bash ]; then exec /bin/bash -l; fi
exec /bin/sh -l`

// Terminal is a root shell on this machine, on a terminal of the given
// size: the same door Run uses, held open for as long as somebody is
// typing into it.
//
// The container is removed when the session ends. If this daemon dies
// first, the container's stdin closes with its connection and the shell
// logs out on its own; Sweep removes whatever a harder crash left.
func (r *Runner) Terminal(ctx context.Context, cols, rows uint16) (*dockerx.TTY, error) {
	if !r.Available() {
		return nil, ErrUnavailable
	}
	tty, err := r.docker.ContainerTTY(ctx, dockerx.ContainerOpts{
		Image:      r.image,
		Entrypoint: []string{"nsenter"},
		Cmd:        []string{"-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "/bin/sh", "-c", hostShell},
		Env: []string{
			"HOME=/root",
			"USER=root",
			"LOGNAME=root",
			"TERM=xterm-256color",
			"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Labels:     map[string]string{ShellLabel: "host"},
		Privileged: true,
		HostPID:    true,
	}, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("start a shell on the host: %w", err)
	}
	log.Printf("hostexec: root shell opened")
	return tty, nil
}

// Hangup signals a process on the host by its host pid. It is how an
// exec inside an app's container is ended when its terminal closed and
// it did not notice — see dockerx.ExecTTY.
func (r *Runner) Hangup(ctx context.Context, pid int, signal string) error {
	res, err := r.Run(ctx, dockerx.SignalCommand(pid, signal)...)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("kill -%s %s: exit %d", signal, strconv.Itoa(pid), res.Code)
	}
	return nil
}

// Sweep removes root shells a previous run of this daemon left behind.
// Every one of them belonged to a connection that is gone.
func (r *Runner) Sweep(ctx context.Context) {
	if !r.Available() {
		return
	}
	running, err := r.docker.RunningContainers(ctx)
	if err != nil {
		return
	}
	for _, c := range running {
		if c.Labels[ShellLabel] == "" {
			continue
		}
		if err := r.docker.RemoveContainer(ctx, c.ID); err != nil {
			log.Printf("hostexec: removing a leftover shell %s: %v", c.Name, err)
		}
	}
}

// ContainerShell is a shell inside a running container on this machine.
//
// It lives here rather than beside ExecTTY because of what happens when
// the terminal closes on a program that did not notice: ending that
// program takes a signal from the host, and this is the package that
// reaches the host. A daemon that cannot (`make dev`) still opens the
// shell, and leaves such a program running.
func (r *Runner) ContainerShell(ctx context.Context, containerID string, cols, rows uint16) (*dockerx.TTY, error) {
	if r == nil || r.docker == nil {
		return nil, ErrUnavailable
	}
	has, err := r.docker.HasPath(ctx, containerID, "/bin/sh")
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, dockerx.ErrNoShell
	}
	var hangup dockerx.Hangup
	if r.Available() {
		hangup = r.Hangup
	}
	return r.docker.ExecTTY(ctx, containerID, dockerx.ShellCommand, dockerx.ShellEnv, cols, rows, hangup)
}
