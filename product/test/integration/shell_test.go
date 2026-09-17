//go:build integration

// A shell, on a real Docker daemon.
//
// The unit tests prove the wire: what a terminal sends reaches a program
// and what the program prints comes back. They cannot prove the two
// things that only exist on Linux with an Engine — that an exec on a TTY
// is what an interactive shell expects, and that nsenter from a
// privileged container lands in the host's own namespaces.

package integration

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
)

// converse types a script into a terminal and reads everything it
// prints until the program exits.
func converse(t *testing.T, tty *dockerx.TTY, script string) (string, int) {
	t.Helper()
	defer tty.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if _, err := tty.Write([]byte(script)); err != nil {
		t.Fatalf("type into the terminal: %v", err)
	}
	out, _ := io.ReadAll(tty)
	code, err := tty.Wait(ctx)
	if err != nil {
		t.Fatalf("wait for the shell: %v", err)
	}
	return string(out), code
}

func TestAShellInAContainer(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	ctx := context.Background()
	if err := exec.Command("docker", "pull", "-q", "busybox").Run(); err != nil {
		t.Fatalf("pull busybox: %v", err)
	}
	id, err := docker.CreateContainer(ctx, dockerx.ContainerOpts{Image: "busybox", Cmd: []string{"sleep", "600"}})
	if err != nil {
		t.Fatalf("create a container: %v", err)
	}
	t.Cleanup(func() { docker.RemoveContainer(context.Background(), id) })
	if err := docker.StartContainer(ctx, id); err != nil {
		t.Fatalf("start it: %v", err)
	}

	host := hostexec.NewRunner(docker, "busybox", true)
	tty, err := host.ContainerShell(ctx, id, 100, 30)
	if err != nil {
		t.Fatalf("open a shell: %v", err)
	}
	// $COLUMNS is not set by a shell on its own, so the size is read
	// from the terminal the Engine allocated, which is what a
	// full-screen program reads too.
	out, code := converse(t, tty, "echo sum=$((40+2)); stty size; exit 3\n")
	if !strings.Contains(out, "sum=42") {
		t.Errorf("the shell did not run what was typed:\n%s", out)
	}
	if !strings.Contains(out, "30 100") {
		t.Errorf("the terminal was not the size asked for:\n%s", out)
	}
	if code != 3 {
		t.Errorf("exited %d, want 3", code)
	}
}

func TestAnImageWithNoShellIsRefused(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	ctx := context.Background()
	if err := exec.Command("docker", "pull", "-q", "registry:2").Run(); err != nil {
		t.Fatalf("pull an image: %v", err)
	}
	// The registry image is Alpine and has a shell; what has none is a
	// scratch image, built here from one static binary so nothing has
	// to be pulled that might change.
	dir := t.TempDir()
	os.WriteFile(dir+"/Dockerfile", []byte("FROM registry:2 AS source\nFROM scratch\nCOPY --from=source /bin/registry /registry\nENTRYPOINT [\"/registry\", \"serve\", \"/etc/docker/registry/config.yml\"]\n"), 0o644)
	if out, err := exec.Command("docker", "build", "-q", "-t", "cubeship-test-noshell", dir).CombinedOutput(); err != nil {
		t.Fatalf("build a shell-less image: %v\n%s", err, out)
	}
	id, err := docker.CreateContainer(ctx, dockerx.ContainerOpts{Image: "cubeship-test-noshell"})
	if err != nil {
		t.Fatalf("create a container: %v", err)
	}
	t.Cleanup(func() { docker.RemoveContainer(context.Background(), id) })

	host := hostexec.NewRunner(docker, "busybox", true)
	if _, err := host.ContainerShell(ctx, id, 80, 24); err != dockerx.ErrNoShell {
		t.Errorf("got %v, want %v", err, dockerx.ErrNoShell)
	}
}

// The machine's own shell: nsenter from a privileged container in the
// host's PID namespace. What proves it is the host is the hostname, which
// a container has its own of.
func TestARootShellIsTheHost(t *testing.T) {
	docker, err := dockerx.New()
	if err != nil {
		t.Fatalf("connect to docker: %v", err)
	}
	if err := exec.Command("docker", "pull", "-q", "busybox").Run(); err != nil {
		t.Fatalf("pull busybox: %v", err)
	}
	want, err := os.Hostname()
	if err != nil {
		t.Fatalf("read this machine's hostname: %v", err)
	}

	host := hostexec.NewRunner(docker, "busybox", true)
	tty, err := host.Terminal(context.Background(), 80, 24)
	if err != nil {
		t.Fatalf("open a root shell: %v", err)
	}
	out, code := converse(t, tty, "echo host=$(hostname) user=$(id -u); exit\n")
	if !strings.Contains(out, "host="+want) {
		t.Errorf("the shell is not on the host (want %s):\n%s", want, out)
	}
	if !strings.Contains(out, "user=0") {
		t.Errorf("the shell is not root:\n%s", out)
	}
	if code != 0 {
		t.Errorf("exited %d", code)
	}
}
