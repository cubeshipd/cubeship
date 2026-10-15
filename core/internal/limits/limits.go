// Package limits is the vocabulary for what a container may take from
// the machine it runs on: a CPU quota and a memory ceiling.
//
// Its own package because it is one idea two modules have — an app's
// container and a database's are both a cgroup with a ceiling — and
// neither of them should have to import the other to say so. Same shape
// as `envvar` and `slug`: small, shared, and about nothing but itself.
package limits

import (
	"errors"

	"cubeship/internal/platform/dockerx"
)

// Limits is the ceiling a container runs under.
//
// **It is per container, not per app.** Three replicas under a one-core
// limit may take three cores between them. That is the only arithmetic
// that survives the replica count changing, and it is what every
// scheduler that has both numbers does.
//
// **Zero is no limit**, in either half and independently: capping memory
// and leaving CPU alone is an ordinary thing to want.
type Limits struct {
	// CPU is cores, and fractional on purpose — half a core is an
	// ordinary answer on a box this size. It is a ceiling rather than a
	// share: a container at its limit is throttled, not merely
	// preferred less when the machine is busy.
	CPU float64 `json:"cpu"`
	// Memory is a hard ceiling in bytes. The kernel enforces it by
	// killing the process that crosses it, which is why **lowering one
	// below what a container is already holding kills it on the spot**
	// — the one thing about this setting that surprises people.
	Memory int64 `json:"memory_bytes"`
}

// None reports whether nothing is capped.
func (l Limits) None() bool { return l.CPU == 0 && l.Memory == 0 }

// Resources is this ceiling in the Engine's own units.
func (l Limits) Resources() dockerx.Resources {
	return dockerx.Resources{
		NanoCPUs:    int64(l.CPU * 1e9),
		MemoryBytes: l.Memory,
	}
}

// MinCPU and MinMemory are the smallest ceilings that mean anything.
//
// The memory floor is the Engine's own — it refuses less than 6 MiB,
// because a cgroup below it cannot hold the runtime that would be
// started inside it. The CPU floor is a hundredth of a core, which is
// the resolution Docker's own --cpus works to; anything smaller would
// be rounded to zero and read as "no limit", which is the opposite of
// what somebody typing a very small number asked for.
const (
	MinCPU    = 0.01
	MinMemory = 6 << 20
)

// ErrInvalid is a ceiling this instance will not set. It names both
// floors rather than the one that was crossed, because whichever it is
// the other is the next thing to get wrong.
var ErrInvalid = errors.New("a limit is either zero, meaning none, or at least 0.01 of a core and 6MiB of memory")

// Valid reports whether this is a ceiling the Engine would accept.
func (l Limits) Valid() bool {
	if l.CPU < 0 || l.Memory < 0 {
		return false
	}
	if l.CPU != 0 && l.CPU < MinCPU {
		return false
	}
	if l.Memory != 0 && l.Memory < MinMemory {
		return false
	}
	return true
}
