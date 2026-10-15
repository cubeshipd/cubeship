package machine

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Where the machine's own numbers are.
const (
	// ProcRoot is the procfs this process sees.
	ProcRoot = "/proc"
	// HostProcRoot is where the daemon's container is given the
	// machine's procfs, read-only. See install.sh.
	HostProcRoot = "/host/proc"
)

// noHostProc is what to do about it, said once because it is the one
// thing an operator can act on here.
const noHostProc = "this daemon's container was started without the machine's /proc mounted at " +
	HostProcRoot + ", and a container's own /proc/net is its network namespace rather than the host's. " +
	"Re-run install.sh to replace the container with one that has it."

// Reader takes the machine's numbers out of the kernel.
//
// **Which file to read is decided once, at startup, and it depends on
// how this daemon is running.** /proc/stat and /proc/meminfo are not
// namespaced, so a container reading its own /proc gets the machine's
// CPU and memory — that is why `top` inside a container shows the
// host's. /proc/net is namespaced, so the same trick gives a container
// its own veth and calls it the instance's traffic. That number would
// be wrong and would look right, which is the only kind of wrong worth
// building a mount for.
type Reader struct {
	// proc is the root the CPU and memory numbers come from.
	proc string
	// netdev is the file whose counters are the *machine's* interfaces.
	// Empty when this daemon cannot see them, and netReason says why.
	netdev    string
	netReason string
	// disk is the path whose filesystem is reported. The data
	// directory, because that is the disk everything this instance
	// keeps is on.
	disk string
}

// NewReader decides where to read from.
//
// On a daemon that is a host process — `make dev` — its own namespaces
// are the machine's, so /proc answers everything. In a container it
// answers all but the network, and the machine's procfs mounted at
// HostProcRoot is what answers that: PID 1's entry there is init, which
// is in the host's network namespace by definition. Reading
// `/host/proc/net/dev` instead would go through /proc/self and land
// back in this container.
func NewReader(dataDir string, inContainer bool) *Reader {
	return newReaderAt(ProcRoot, HostProcRoot, dataDir, inContainer)
}

// newReaderAt is NewReader with the two roots given, so the decision
// above can be tested against a directory of fixtures rather than
// against whatever machine the tests are running on.
func newReaderAt(proc, hostProc, dataDir string, inContainer bool) *Reader {
	r := &Reader{proc: proc, disk: dataDir}
	if !inContainer {
		r.netdev = filepath.Join(proc, "net", "dev")
		return r
	}
	hostNet := filepath.Join(hostProc, "1", "net", "dev")
	if _, err := os.ReadFile(hostNet); err == nil {
		r.proc = hostProc
		r.netdev = hostNet
		return r
	}
	r.netReason = noHostProc
	return r
}

// CPU is the machine's cumulative CPU time.
func (r *Reader) CPU() (CPUTime, error) {
	data, err := os.ReadFile(filepath.Join(r.proc, "stat"))
	if err != nil {
		return CPUTime{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return ParseCPU(data)
}

// Cores is how many the machine has, or 0 when that cannot be read.
func (r *Reader) Cores() int {
	data, err := os.ReadFile(filepath.Join(r.proc, "stat"))
	if err != nil {
		return 0
	}
	return ParseCores(data)
}

// Memory is what the machine has and what is spoken for.
func (r *Reader) Memory() (Memory, error) {
	data, err := os.ReadFile(filepath.Join(r.proc, "meminfo"))
	if err != nil {
		return Memory{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return ParseMemory(data)
}

// DiskPath is which filesystem the disk numbers describe.
func (r *Reader) DiskPath() string { return r.disk }

// Disk is the filesystem the data directory is on.
//
// Statfs rather than anything counted per directory: what an operator
// needs to know is whether the next image pull fits, and that is a fact
// about the filesystem — not the sum of what Cubeship happens to have
// put on it.
func (r *Reader) Disk() (Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(r.disk, &st); err != nil {
		return Disk{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	block := uint64(st.Bsize)
	total := uint64(st.Blocks) * block
	free := uint64(st.Bfree) * block
	return Disk{Used: int64(total - free), Total: int64(total)}, nil
}

// Network is the machine's cumulative interface counters, and which
// interfaces they were added up from.
func (r *Reader) Network() (Net, []string, error) {
	if r.netdev == "" {
		return Net{}, nil, fmt.Errorf("%w: %s", ErrUnavailable, r.netReason)
	}
	data, err := os.ReadFile(r.netdev)
	if err != nil {
		return Net{}, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	counters, interfaces := ParseNetDev(data)
	if len(interfaces) == 0 {
		return Net{}, nil, fmt.Errorf("%w: %s has no interface that is not a loopback or a Docker bridge", ErrUnavailable, r.netdev)
	}
	return counters, interfaces, nil
}

// Unavailable is what this daemon cannot measure, and why.
//
// Probed rather than remembered from the last pass: it is read when
// somebody opens the screen, and the answer it gives is the sentence
// under the missing chart. All four probes are a file read and a
// statfs — cheap enough to do honestly.
func (r *Reader) Unavailable() map[string]string {
	out := map[string]string{}
	if _, err := r.CPU(); err != nil {
		out[MeasureCPU] = reason(err)
	}
	if _, err := r.Memory(); err != nil {
		out[MeasureMemory] = reason(err)
	}
	if _, err := r.Disk(); err != nil {
		out[MeasureDisk] = reason(err)
	}
	if _, _, err := r.Network(); err != nil {
		out[MeasureNetwork] = reason(err)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// reason is the error without the wrapper this package added to make it
// recognisable — what is shown is a sentence for a person, not a Go
// error chain.
func reason(err error) string {
	msg := err.Error()
	if prefix := ErrUnavailable.Error() + ": "; len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
		return msg[len(prefix):]
	}
	return msg
}
