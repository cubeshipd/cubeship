package machine

import (
	"strings"
	"testing"
)

// A real /proc/stat, trimmed to the lines that matter.
const procStat = `cpu  100 20 30 800 40 0 10 5 60 3
cpu0 50 10 15 400 20 0 5 2 30 1
cpu1 50 10 15 400 20 0 5 3 30 2
intr 12345 0 0
ctxt 987654
btime 1757000000
`

// The two fields at the end of the aggregate line are the trap.
//
// `guest` and `guest_nice` are already inside `user` and `nice` — the
// kernel counts a guest tick in both — so adding them makes the total
// bigger than the time that actually passed, which reports a busy
// machine as idle. Anything running VMs, which is to say any machine
// somebody might install this on.
func TestGuestTimeIsNotCountedTwice(t *testing.T) {
	got, err := ParseCPU([]byte(procStat))
	if err != nil {
		t.Fatal(err)
	}
	// user+nice+system+idle+iowait+irq+softirq+steal, and nothing after.
	const wantTotal = 100 + 20 + 30 + 800 + 40 + 0 + 10 + 5
	// Everything but idle and iowait.
	const wantBusy = 100 + 20 + 30 + 0 + 10 + 5
	if got.Total != wantTotal || got.Busy != wantBusy {
		t.Errorf("ParseCPU = %+v, want total %d and busy %d", got, wantTotal, wantBusy)
	}
}

// A machine waiting on its disk is not a machine short of CPU. Reading
// iowait as busy is how a slow disk gets diagnosed as a small box, so
// it counts as idle — which is what `top` does with it too.
func TestWaitingOnTheDiskIsNotBusy(t *testing.T) {
	quiet := "cpu  0 0 0 100 900 0 0 0\n"
	got, err := ParseCPU([]byte(quiet))
	if err != nil {
		t.Fatal(err)
	}
	if got.Busy != 0 {
		t.Errorf("a machine doing nothing but waiting reported %d busy ticks", got.Busy)
	}
}

func TestTheCoresAreCountedFromTheMachineNotTheProcess(t *testing.T) {
	if got := ParseCores([]byte(procStat)); got != 2 {
		t.Errorf("ParseCores = %d, want 2", got)
	}
	// The aggregate line is not a core, and neither is anything else
	// that merely starts with those three letters.
	if got := ParseCores([]byte("cpu 1 2 3\ncpuidle 0\n")); got != 0 {
		t.Errorf("ParseCores counted %d cores in a file with none", got)
	}
}

const procMeminfo = `MemTotal:        2048000 kB
MemFree:          100000 kB
MemAvailable:    1024000 kB
Buffers:           50000 kB
Cached:           800000 kB
`

// Used is total minus *available*, not total minus free.
//
// Total minus free counts the page cache as used, and the page cache is
// every file the machine has ever read — so a box with 1 GiB genuinely
// spoken for would report itself at 95% and stay there, which is a
// monitoring screen nobody can act on.
func TestMemoryUsedIsWhatIsActuallySpokenFor(t *testing.T) {
	got, err := ParseMemory([]byte(procMeminfo))
	if err != nil {
		t.Fatal(err)
	}
	const kB = 1024
	if got.Total != 2048000*kB {
		t.Errorf("total = %d", got.Total)
	}
	if want := int64((2048000 - 1024000) * kB); got.Used != want {
		t.Errorf("used = %d, want %d — total minus MemAvailable", got.Used, want)
	}

	if _, err := ParseMemory([]byte("MemTotal: 2048000 kB\n")); err == nil {
		t.Error("a meminfo with no MemAvailable was accepted")
	}
}

const procNetDev = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000 10 0 0 0 0 0 0 1000 10 0 0 0 0 0 0
  eth0: 5000 50 0 0 0 0 0 0 2000 20 0 0 0 0 0 0
docker0: 9999 99 0 0 0 0 0 0 9999 99 0 0 0 0 0 0
br-1a2b3c: 8888 88 0 0 0 0 0 0 8888 88 0 0 0 0 0 0
vethb1a2c3: 7777 77 0 0 0 0 0 0 7777 77 0 0 0 0 0 0
   wg0: 300 3 0 0 0 0 0 0 100 1 0 0 0 0 0 0
`

// The bridges and the veths are the same bytes a second and a third
// time: traffic to an app arrives on the machine's interface and is
// then forwarded across a Docker bridge and a veth pair into the
// container. Adding all of them up would report three times the
// traffic and call it the instance's.
func TestOnlyTheMachinesOwnInterfacesAreCounted(t *testing.T) {
	got, interfaces := ParseNetDev([]byte(procNetDev))
	if want := strings.Join([]string{"eth0", "wg0"}, ","); strings.Join(interfaces, ",") != want {
		t.Fatalf("counted %v, want %s", interfaces, want)
	}
	if got.Rx != 5300 || got.Tx != 2100 {
		t.Errorf("ParseNetDev = %+v, want rx 5300 and tx 2100", got)
	}
}

// A tunnel is somebody's actual route to the world. Leaving wg0 and
// tun0 out would report nothing at all on a box that reaches its
// network that way, and reporting nothing is not the same as reporting
// zero.
func TestATunnelCountsAndALoopbackDoesNot(t *testing.T) {
	counted := map[string]bool{
		"eth0": true, "ens3": true, "enp1s0": true, "wg0": true, "tun0": true, "bond0": true,
		"lo": false, "docker0": false, "br-1a2b3c": false, "veth9f": false, "virbr0": false, "": false,
	}
	for name, want := range counted {
		if got := Counted(name); got != want {
			t.Errorf("Counted(%q) = %v, want %v", name, got, want)
		}
	}
}

// A counter that went backwards is a reboot or an interface that was
// recreated. There is no rate across that, and the arithmetic that
// would produce one produces an enormous number — a spike on the chart
// at exactly the moment somebody is looking for the cause of one.
func TestACounterThatWentBackwardsHasNoRate(t *testing.T) {
	if _, ok := Rate(9000, 10, 30); ok {
		t.Error("a rate was computed across a counter reset")
	}
	if _, ok := Rate(0, 100, 0); ok {
		t.Error("a rate was computed over no time at all")
	}
	got, ok := Rate(1000, 4000, 30)
	if !ok || got != 100 {
		t.Errorf("Rate = %v (%v), want 100 bytes a second", got, ok)
	}
}

// The machine's percentage is of the whole machine — the opposite
// convention from a container's, where 100 is one core. Both are right
// where they are: what share of the host one thing is taking, and how
// much of the host is left.
func TestTheMachinesCPUIsAShareOfTheWholeMachine(t *testing.T) {
	// Half of every tick busy, whatever the core count.
	previous := CPUTime{Busy: 0, Total: 0}
	current := CPUTime{Busy: 500, Total: 1000}
	if got := cpuPercent(previous, current); got != 50 {
		t.Errorf("cpuPercent = %v, want 50", got)
	}
	// A reboot: the counters restart, and no rate is the honest answer.
	if got := cpuPercent(CPUTime{Busy: 900, Total: 1000}, CPUTime{Busy: 5, Total: 10}); got != 0 {
		t.Errorf("cpuPercent across a reboot = %v, want 0", got)
	}
	// Two readings with nothing between them.
	if got := cpuPercent(current, current); got != 0 {
		t.Errorf("cpuPercent with no elapsed ticks = %v, want 0", got)
	}
}
