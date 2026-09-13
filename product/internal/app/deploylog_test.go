package app

import (
	"strings"
	"sync"
	"testing"
)

type recorder struct {
	mu     sync.Mutex
	writes []string
}

func (r *recorder) save(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, text)
}

func (r *recorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.writes) == 0 {
		return ""
	}
	return r.writes[len(r.writes)-1]
}

// Closing writes whatever is left, however the deploy ended. A build's
// last lines are the ones explaining it, so losing them on failure would
// lose the only explanation there is.
func TestClosingWritesTheOutput(t *testing.T) {
	rec := &recorder{}
	l := newDeploymentLog(rec.save)

	l.Write([]byte("step 1\n"))
	l.Write([]byte("step 2\n"))
	l.Close()

	if got := rec.last(); got != "step 1\nstep 2\n" {
		t.Errorf("saved %q", got)
	}
}

// Closing twice must not write twice or panic: the deploy path closes it
// on the way out of every branch.
func TestClosingIsIdempotent(t *testing.T) {
	rec := &recorder{}
	l := newDeploymentLog(rec.save)
	l.Write([]byte("x"))
	l.Close()
	l.Close()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.writes) != 1 {
		t.Errorf("wrote %d times, want 1", len(rec.writes))
	}
}

// Nothing to say, nothing written. A deploy that pulls an image should
// not update its row with an empty string.
func TestNothingWrittenMeansNoUpdate(t *testing.T) {
	rec := &recorder{}
	newDeploymentLog(rec.save).Close()

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.writes) != 0 {
		t.Errorf("wrote %d times for a build that printed nothing", len(rec.writes))
	}
}

// A build that prints without limit must not put megabytes in a row that
// gets read back into a browser. The tail survives, because the reason
// it failed is at the end.
func TestOutputIsCappedKeepingTheEnd(t *testing.T) {
	rec := &recorder{}
	l := newDeploymentLog(rec.save)

	for i := 0; i < 40000; i++ {
		l.Write([]byte("noisy line of build output\n"))
	}
	l.Write([]byte("the reason it failed\n"))
	l.Close()

	saved := rec.last()
	if len(saved) > MaxDeploymentLogBytes+200 {
		t.Errorf("saved %d bytes, want around %d", len(saved), MaxDeploymentLogBytes)
	}
	if !strings.HasSuffix(saved, "the reason it failed\n") {
		t.Error("the end of the output was dropped instead of the start")
	}
	if !strings.Contains(saved, "earlier output dropped") {
		t.Error("the truncation is silent; a reader would think this was the whole build")
	}
	// Truncation lands on a line boundary rather than mid-word.
	body := strings.TrimPrefix(saved, saved[:strings.IndexByte(saved, '\n')+1])
	if first, _, _ := strings.Cut(body, "\n"); first != "" && first != "noisy line of build output" {
		t.Errorf("the first surviving line is a fragment: %q", first)
	}
}
