// Package hostexec runs a command on the host from inside the daemon's
// container.
//
// It exists for one thing the daemon cannot otherwise reach: a firewall.
// `ufw` is a host program that edits the host's netfilter tables and
// keeps its state in the host's /etc, and none of that is visible from a
// container on a bridge network.
//
// **It adds no privilege the daemon does not already hold.** The daemon
// runs with /var/run/docker.sock mounted, which is root on the host by
// any other name — anything that can create containers can create a
// privileged one. So this is not a new door; it is the existing door,
// used deliberately and in one place, rather than left as something any
// future module might reinvent worse.
//
// The mechanism is the ordinary one: a throwaway container in the host's
// PID namespace, privileged, running `nsenter -t 1 -m -u -i -n -p --`
// against PID 1. What runs is then the host's own binary, with the
// host's filesystem, mounts and network — not a copy of the tool inside
// a container writing rules into a namespace nobody routes through.
//
// The image is the daemon's own. It is on the box by definition, it is
// Alpine, and busybox carries nsenter — so there is no third image in
// the release and nothing to pull the first time a firewall rule is
// written.
package hostexec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cubeship/internal/platform/dockerx"
)

// ErrUnavailable is a daemon that cannot reach the host at all: it is
// running as a host process itself (`make dev`), or nothing told it
// which image to use.
//
// Distinguished from a command that failed, because the two mean
// different things to whoever is reading a screen: one is "this instance
// cannot do that", the other is "the host said no".
var ErrUnavailable = errors.New("this daemon cannot run commands on the host")

// Runner runs commands in the host's namespaces.
type Runner struct {
	docker *dockerx.Client
	// image is what the throwaway container runs. The daemon's own, so
	// nothing has to be pulled.
	image string
	// enabled is false for a daemon that is not in a container. There
	// it is already on the host — but as an unprivileged process, and
	// pretending otherwise would produce failures that read like a
	// broken firewall rather than a development setup.
	enabled bool
}

func NewRunner(docker *dockerx.Client, image string, inContainer bool) *Runner {
	return &Runner{docker: docker, image: image, enabled: inContainer && image != ""}
}

// Available reports whether Run can do anything at all, so a caller can
// say so on a screen rather than offering a button that always fails.
func (r *Runner) Available() bool {
	return r != nil && r.enabled && r.docker != nil
}

// Result is what the host said.
type Result struct {
	// Output is stdout and stderr together. A tool's refusal is usually
	// on stderr, and it is the part worth showing.
	Output string
	// Code is the command's exit status. Non-zero is not an error here:
	// `ufw status` on a host without ufw exits non-zero and that is an
	// answer, not a failure.
	Code int
}

// OK reports a command that ran and succeeded.
func (r Result) OK() bool { return r.Code == 0 }

// Run executes argv in the host's namespaces and waits for it.
//
// A non-zero exit is returned in the Result rather than as an error:
// "the host refused" and "the daemon could not ask" are different
// things, and collapsing them loses exactly the sentence somebody needs.
func (r *Runner) Run(ctx context.Context, argv ...string) (Result, error) {
	if !r.Available() {
		return Result{}, ErrUnavailable
	}
	if len(argv) == 0 {
		return Result{}, errors.New("hostexec: nothing to run")
	}

	// -t 1 is PID 1, which is the host's init because the container is
	// in the host's PID namespace. The five flags are its mount, UTS,
	// IPC, network and PID namespaces — everything that makes the
	// command indistinguishable from one typed over SSH.
	cmd := append([]string{"-t", "1", "-m", "-u", "-i", "-n", "-p", "--"}, argv...)

	output, code, err := r.docker.RunOneShot(ctx, dockerx.ContainerOpts{
		Image: r.image,
		// The image's entrypoint is the daemon. Left alone, this would
		// start a second one.
		Entrypoint: []string{"nsenter"},
		Cmd:        cmd,
		Privileged: true,
		HostPID:    true,
		AutoRemove: true,
		// No name: two of these may legitimately overlap, and a fixed
		// name would make the second fail on a conflict.
	})
	if err != nil {
		return Result{}, fmt.Errorf("run %s on the host: %w", argv[0], err)
	}
	return Result{Output: strings.TrimRight(output, "\n"), Code: code}, nil
}

// Script runs a shell line on the host, for the cases that are genuinely
// a pipeline or a redirect rather than one program with arguments.
//
// Nothing user-supplied is ever interpolated into one of these. Every
// caller in this repository passes a constant, and a value that has to
// reach the host goes through Run's argv, where the shell never sees it.
func (r *Runner) Script(ctx context.Context, line string) (Result, error) {
	return r.Run(ctx, "sh", "-c", line)
}
