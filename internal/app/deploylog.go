package app

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MaxDeploymentLogBytes bounds what one deployment keeps. A build that
// prints without limit — a compiler with warnings on, a package manager
// with progress bars — would otherwise put megabytes in a row that gets
// read back into a browser.
//
// The tail is what survives, because the reason a build failed is at the
// end of what it printed.
const MaxDeploymentLogBytes = 256 * 1024

// deploymentLogFlushInterval is how often output reaches the row while a
// build is still running. It matches the rate a dashboard polls a
// deployment at, so a build is watchable rather than a blank wait
// followed by everything at once.
const deploymentLogFlushInterval = 2 * time.Second

// deploymentLog collects a build's output and writes it to the
// deployment it belongs to.
//
// Writing on a timer rather than per line is deliberate: BuildKit emits
// output in small pieces, and an UPDATE per piece would make a noisy
// build heavier on the database than on the builder.
type deploymentLog struct {
	mu      sync.Mutex
	buf     strings.Builder
	dirty   bool
	dropped bool

	save func(text string)
	done chan struct{}
	once sync.Once
}

func newDeploymentLog(save func(text string)) *deploymentLog {
	l := &deploymentLog{save: save, done: make(chan struct{})}
	go l.flushLoop()
	return l
}

func (l *deploymentLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buf.Write(p)
	l.dirty = true
	if l.buf.Len() > MaxDeploymentLogBytes {
		// Keep the tail. Rebuilding the buffer is O(n) but only happens
		// once the cap is exceeded, by which point the build is already
		// the slow part.
		s := l.buf.String()
		s = s[len(s)-MaxDeploymentLogBytes:]
		if i := strings.IndexByte(s, '\n'); i >= 0 && i+1 < len(s) {
			s = s[i+1:] // start on a line boundary
		}
		l.buf.Reset()
		l.buf.WriteString(s)
		l.dropped = true
	}
	return len(p), nil
}

func (l *deploymentLog) flushLoop() {
	ticker := time.NewTicker(deploymentLogFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.flush()
		case <-l.done:
			return
		}
	}
}

func (l *deploymentLog) flush() {
	l.mu.Lock()
	if !l.dirty {
		l.mu.Unlock()
		return
	}
	text := l.text()
	l.dirty = false
	l.mu.Unlock()

	l.save(text)
}

// text renders what has been collected. Caller holds the lock.
func (l *deploymentLog) text() string {
	if l.dropped {
		return "[earlier output dropped: this build printed more than " +
			"Cubeship keeps]\n" + l.buf.String()
	}
	return l.buf.String()
}

// Close stops the timer and writes whatever is left. A build's last
// lines are the ones explaining how it ended, so this must run even when
// the deploy failed.
func (l *deploymentLog) Close() {
	l.once.Do(func() {
		close(l.done)
		l.flush()
	})
}

// saveDeploymentLogs is the writer's escape hatch to the database. It
// takes its own context: the deploy's may already be cancelled, and
// losing the explanation of why is exactly the wrong thing to lose.
func (o *Orchestrator) saveDeploymentLogs(deploymentID int64) func(string) {
	return func(text string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = o.apps.SetDeploymentLogs(ctx, deploymentID, text)
	}
}
