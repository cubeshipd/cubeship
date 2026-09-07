// Package machine is what the box this instance runs on is doing:
// its CPU, its memory, the disk everything here is kept on, and the
// bytes moving over its own network interfaces.
//
// It is a module beside `metrics` rather than a fourth kind inside it.
// That one answers one question about a container — a CPU and a
// resident set, sampled through the Engine — and this one answers a
// different set of measurements about something that is not a
// container: there is no cgroup for a disk filling up, and the Engine
// has no opinion about the wire. What the two share is cadence, windows
// and bucketing, and those are imported rather than copied.
//
// **Everything here is read, never configured.** Same shape as
// `certificates`, which reads Traefik's store, and `firewall`, which
// reads the host's ufw: the numbers live in the kernel, this module
// takes them at an interval and remembers them long enough to draw a
// line.
//
// The API calls it the instance — `/instance/metrics` — because on this
// product they are the same box, and "instance" is the word every other
// surface uses for it. The package is named for what it reads rather
// than for what the product calls it, because `host` is already two
// other things here: the machine a firewall rule is executed on, and
// the name an app answers at.
package machine

import (
	"errors"
	"time"
)

// The measurements, by the names the API reports them missing under.
const (
	MeasureCPU     = "cpu"
	MeasureMemory  = "memory"
	MeasureDisk    = "disk"
	MeasureNetwork = "network"
)

// Sample is one reading of the machine, already turned into the numbers
// a chart shows.
type Sample struct {
	At time.Time `json:"at"`
	// CPUPercent is percent of the **whole machine**: 100 is every core
	// busy. Deliberately not the convention metrics uses for a
	// container, where 100 is one core — there the question is how much
	// of the host one thing is taking, and here it is how much of the
	// host is left.
	CPUPercent float64 `json:"cpu_percent"`
	// MemoryBytes is total minus available, which is what the kernel
	// says is actually spoken for — not total minus free, which counts
	// page cache as used and makes every machine look full.
	MemoryBytes      int64 `json:"memory_bytes"`
	MemoryTotalBytes int64 `json:"memory_total_bytes"`
	// DiskBytes and DiskTotalBytes are the filesystem the data
	// directory is on, counted the way df counts: used includes the
	// blocks reserved for root, because they are not free for this.
	DiskBytes      int64 `json:"disk_bytes"`
	DiskTotalBytes int64 `json:"disk_total_bytes"`
	// RxBytesPerSec and TxBytesPerSec are averaged over the interval
	// since the previous reading. Absent, rather than zero, on a daemon
	// that cannot see the host's interfaces — and absent on the first
	// pass after a restart, because a rate is a difference and there is
	// nothing yet to subtract from.
	RxBytesPerSec *float64 `json:"rx_bytes_per_sec,omitempty"`
	TxBytesPerSec *float64 `json:"tx_bytes_per_sec,omitempty"`
}

// Series is the machine's readings over one window, with the facts a
// caller would otherwise have to derive to draw axes.
type Series struct {
	Window  string   `json:"window"`
	Samples []Sample `json:"samples"`

	// Cores is how many the machine has, which is what says whether 60%
	// is a busy machine or a quiet one.
	Cores int `json:"cores"`
	// MemoryTotalBytes and DiskTotalBytes are the ceilings, reported
	// even when nothing has been sampled yet — they are facts about the
	// machine rather than about the series.
	MemoryTotalBytes int64 `json:"memory_total_bytes"`
	DiskTotalBytes   int64 `json:"disk_total_bytes"`
	// DiskPath is which filesystem the disk numbers are about. Said
	// rather than assumed: a box with the data directory on its own
	// volume is a normal box, and "the disk" would be a lie on it.
	DiskPath string `json:"disk_path"`
	// Interfaces are the ones the network numbers add up, so a figure
	// that looks wrong can be explained rather than argued with.
	Interfaces []string `json:"interfaces,omitempty"`

	// Unavailable is what this daemon cannot measure, and why, keyed by
	// the measurement. Empty when it can measure everything.
	//
	// A map rather than four booleans, because what a caller needs is
	// the sentence: a missing chart with no reason beside it is a bug
	// report, and the reason here is usually an instruction — the
	// daemon's container was started without the host's procfs.
	Unavailable map[string]string `json:"unavailable,omitempty"`
}

// ErrUnavailable is a measurement this daemon cannot take at all.
var ErrUnavailable = errors.New("this daemon cannot read that from the machine it runs on")
