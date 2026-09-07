package machine

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// The kernel's own numbers, parsed. Every function here takes the bytes
// rather than a path, so the awkward part — which column is which, and
// which of them lie — is testable against a fixture instead of against
// whatever machine the tests happen to run on.

// CPUTime is the aggregate line of /proc/stat: cumulative ticks since
// boot, which mean nothing on their own and everything next to the
// previous reading.
type CPUTime struct {
	// Busy is every mode that is not idle, and Total includes idle.
	Busy  uint64
	Total uint64
}

// ParseCPU reads the aggregate `cpu` line of /proc/stat.
//
// The first eight fields only: user, nice, system, idle, iowait, irq,
// softirq, steal. `guest` and `guest_nice` come after them and are
// **already counted inside user and nice** — adding them would inflate
// the total on a machine running VMs, which is to say make a busy host
// look idle.
//
// iowait counts as idle. A machine waiting on its disk is not a machine
// short of CPU, and reading it as busy is how a slow disk gets
// diagnosed as a small box.
func ParseCPU(data []byte) (CPUTime, error) {
	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		var out CPUTime
		for i, f := range fields[1:] {
			if i >= 8 {
				break
			}
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return CPUTime{}, fmt.Errorf("cpu field %d: %w", i, err)
			}
			out.Total += v
			// Fields 3 and 4 are idle and iowait.
			if i != 3 && i != 4 {
				out.Busy += v
			}
		}
		return out, nil
	}
	return CPUTime{}, fmt.Errorf("no aggregate cpu line in /proc/stat")
}

// ParseCores counts the per-core lines of /proc/stat.
//
// From /proc/stat rather than from runtime.NumCPU, which in a container
// with a cpuset reports what the daemon may use rather than what the
// machine has — and this whole module is about the machine.
func ParseCores(data []byte) int {
	cores := 0
	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		name, _, ok := strings.Cut(scan.Text(), " ")
		if !ok || !strings.HasPrefix(name, "cpu") || name == "cpu" {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimPrefix(name, "cpu")); err == nil {
			cores++
		}
	}
	return cores
}

// Memory is what /proc/meminfo says about the machine.
type Memory struct {
	// Used is Total minus MemAvailable — the kernel's own estimate of
	// what a new process could get without swapping. Total minus free
	// is the other answer and it is the wrong one: it counts the page
	// cache as used, so every machine that has ever read a file looks
	// like it is about to run out.
	Used  int64
	Total int64
}

// ParseMemory reads MemTotal and MemAvailable out of /proc/meminfo.
//
// MemAvailable has been there since Linux 3.14 and there is no fallback
// here for older kernels: a Docker Engine new enough to run this is not
// running on one.
func ParseMemory(data []byte) (Memory, error) {
	var total, available int64
	var haveAvailable bool

	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		name, rest, ok := strings.Cut(scan.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		// Every line worth reading here is in kB.
		v *= 1024
		switch name {
		case "MemTotal":
			total = v
		case "MemAvailable":
			available, haveAvailable = v, true
		}
	}
	if total == 0 || !haveAvailable {
		return Memory{}, fmt.Errorf("no MemTotal and MemAvailable in /proc/meminfo")
	}
	return Memory{Used: total - available, Total: total}, nil
}

// Net is the cumulative byte counters of the interfaces worth counting,
// added together.
type Net struct {
	Rx uint64
	Tx uint64
}

// ParseNetDev sums /proc/net/dev over the interfaces this machine
// actually reaches the world through, and reports which those were.
//
// The sum is over real interfaces only, because the rest of that file
// is the same bytes counted again: traffic to an app arrives on the
// machine's own interface and is then forwarded across a Docker bridge
// and a veth pair to the container. Counting all three would report
// three times the traffic and call it the instance's.
func ParseNetDev(data []byte) (Net, []string) {
	var out Net
	var counted []string

	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		name, rest, ok := strings.Cut(scan.Text(), ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if !Counted(name) {
			continue
		}
		fields := strings.Fields(rest)
		// Eight receive columns, then eight transmit ones. Bytes is the
		// first of each.
		if len(fields) < 9 {
			continue
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		out.Rx += rx
		out.Tx += tx
		counted = append(counted, name)
	}
	return out, counted
}

// virtual are the interfaces Docker and friends create. Everything
// behind one of these prefixes carries traffic that has already been
// counted on the interface it arrived at.
var virtual = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "kube", "cali"}

// Counted says whether an interface's bytes are the machine's own
// traffic.
//
// Loopback is out: an app talking to a database on this box is not
// bandwidth anybody is billed for or limited by. A VPN or a tunnel is
// **in** — wg0 and tun0 carry real traffic to somewhere else, and on a
// box that reaches its world that way, leaving them out would report
// nothing at all.
func Counted(name string) bool {
	if name == "" || name == "lo" {
		return false
	}
	for _, prefix := range virtual {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

// Disk is one filesystem, counted the way df counts it.
type Disk struct {
	// Used includes the blocks reserved for root: they are not free for
	// an image pull or a database, which is the question being asked.
	Used  int64
	Total int64
}

// Rate is bytes (or ticks) per second between two cumulative readings.
//
// A counter that went backwards means the machine rebooted or the
// interface was recreated, and the honest answer to "how fast was it
// going across that" is nothing at all — so this reports no rate rather
// than an enormous one.
func Rate(previous, current uint64, elapsed float64) (float64, bool) {
	if elapsed <= 0 || current < previous {
		return 0, false
	}
	return float64(current-previous) / elapsed, true
}
