package user

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	loginConcurrency = 2
	loginBurst       = 10
)

// ErrRateLimited refuses work immediately; login never queues passwords or
// goroutines waiting for an Argon2 allocation.
var ErrRateLimited = errors.New("too many login attempts; try again later")

// Each instance has one user service, shared by all request adapters.
type loginAdmission struct {
	mu     sync.Mutex
	now    func() time.Time
	last   time.Time
	tokens float64
	active int
}

func newLoginAdmission(now func() time.Time) *loginAdmission {
	return &loginAdmission{now: now, last: now(), tokens: loginBurst}
}

func (a *loginAdmission) acquire(ctx context.Context) (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := a.now()
	if elapsed := now.Sub(a.last).Seconds(); elapsed > 0 {
		a.tokens = min(float64(loginBurst), a.tokens+elapsed)
		a.last = now
	}
	if a.active >= loginConcurrency || a.tokens < 1 {
		return nil, ErrRateLimited
	}
	a.active++
	a.tokens--
	return func() { a.mu.Lock(); a.active--; a.mu.Unlock() }, nil
}
