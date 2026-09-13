package machine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bug this file exists to prevent is a number that is wrong and
// looks right.
//
// /proc/stat and /proc/meminfo are not namespaced, so a container
// reading its own gets the machine's — that is why `top` inside one
// shows the host's memory. /proc/net **is** namespaced, so the same
// file read the same way gives the daemon container's own veth: a few
// kilobytes a second of dashboard traffic, drawn on a chart labelled as
// this instance's network. Nobody would ever catch that by looking.
func TestTheInstancesTrafficIsNeverTheDaemonsOwnVeth(t *testing.T) {
	proc, hostProc := t.TempDir(), t.TempDir()
	write(t, filepath.Join(proc, "net", "dev"), procNetDev)
	write(t, filepath.Join(proc, "stat"), procStat)
	// The daemon container's own namespace: one veth and a loopback.
	write(t, filepath.Join(proc, "net", "dev"), `Inter-|
 face |bytes
    lo: 10 1 0 0 0 0 0 0 10 1 0 0 0 0 0 0
eth0@if12: 40 4 0 0 0 0 0 0 20 2 0 0 0 0 0 0
`)
	// The machine's, reached through PID 1 rather than through
	// /proc/net — that one resolves via /proc/self and lands back in
	// this container however it is mounted.
	write(t, filepath.Join(hostProc, "1", "net", "dev"), procNetDev)
	write(t, filepath.Join(hostProc, "stat"), procStat)

	r := newReaderAt(proc, hostProc, t.TempDir(), true)
	counters, interfaces, err := r.Network()
	if err != nil {
		t.Fatalf("the machine's procfs is mounted and its interfaces were not read: %v", err)
	}
	if strings.Join(interfaces, ",") != "eth0,wg0" {
		t.Errorf("counted %v, want the machine's interfaces", interfaces)
	}
	if counters.Rx != 5300 {
		t.Errorf("rx = %d, want the machine's 5300 rather than the container's", counters.Rx)
	}
}

// Without the mount the answer is nothing, with the reason. Not zero,
// and not this container's own counters: an instance that reports no
// traffic is one somebody investigates, and one that reports the wrong
// traffic is one nobody ever does.
func TestAContainerWithoutTheMachinesProcfsRefusesToGuess(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), procStat)
	write(t, filepath.Join(proc, "meminfo"), procMeminfo)
	write(t, filepath.Join(proc, "net", "dev"), procNetDev)

	r := newReaderAt(proc, filepath.Join(t.TempDir(), "absent"), t.TempDir(), true)
	if _, _, err := r.Network(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Network() = %v, want ErrUnavailable", err)
	}

	missing := r.Unavailable()
	if !strings.Contains(missing[MeasureNetwork], "install.sh") {
		t.Errorf("the reason given is %q, and it has to say what to do about it", missing[MeasureNetwork])
	}
	// And the three that do not need the mount still answer, which is
	// the point of reporting a measurement missing rather than the
	// whole screen.
	for _, m := range []string{MeasureCPU, MeasureMemory, MeasureDisk} {
		if reason, absent := missing[m]; absent {
			t.Errorf("%s was reported unavailable (%s) on a daemon that can read it", m, reason)
		}
	}
}

// A daemon that is a host process — `make dev` — is in the machine's
// own namespaces, so its own /proc answers everything and there is no
// mount to want.
func TestADaemonOnTheHostReadsItsOwnProc(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), procStat)
	write(t, filepath.Join(proc, "net", "dev"), procNetDev)

	r := newReaderAt(proc, filepath.Join(t.TempDir(), "absent"), t.TempDir(), false)
	if _, _, err := r.Network(); err != nil {
		t.Fatalf("Network() on a host daemon: %v", err)
	}
	if got := r.Cores(); got != 2 {
		t.Errorf("Cores = %d, want the 2 in the fixture", got)
	}
}

// The disk reported is the filesystem the data directory is on, and
// which one that is has to be said: a box with the data directory on
// its own volume is a normal box, and "the disk" would be a lie on it.
func TestTheDiskIsTheOneTheInstanceKeepsItsThingsOn(t *testing.T) {
	dir := t.TempDir()
	r := newReaderAt(t.TempDir(), t.TempDir(), dir, false)
	if r.DiskPath() != dir {
		t.Errorf("DiskPath = %q, want %q", r.DiskPath(), dir)
	}
	disk, err := r.Disk()
	if err != nil {
		t.Fatalf("statfs on a directory that exists: %v", err)
	}
	if disk.Total <= 0 || disk.Used > disk.Total {
		t.Errorf("Disk = %+v, which is not a filesystem", disk)
	}

	gone := newReaderAt(t.TempDir(), t.TempDir(), filepath.Join(dir, "not-here"), false)
	if _, err := gone.Disk(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("statfs on a path that is not there = %v, want ErrUnavailable", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
