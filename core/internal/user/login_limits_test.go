package user

import (
	"context"
	"cubeship/internal/platform/database"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginBodyBoundBeforeService(t *testing.T) {
	for _, body := range []string{`{"username":"alice","password":"` + strings.Repeat("x", 16385) + `"}`, `{"username":"alice","password":"password"}` + strings.Repeat(" ", 16385)} {
		for _, unknownLength := range []bool{false, true} {
			func() {
				defer func() {
					if v := recover(); v != nil {
						t.Errorf("oversized body reached service: %v", v)
					}
				}()
				r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(body))
				if unknownLength {
					r.ContentLength = -1
				}
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				(&Handler{}).login(w, r)
				if w.Code != http.StatusRequestEntityTooLarge {
					t.Errorf("status = %d; want 413", w.Code)
				}
			}()
		}
	}
}

func TestLoginAdmission(t *testing.T) {
	now := time.Unix(100, 0)
	gate := newLoginAdmission(func() time.Time { return now })
	for i := 0; i < loginBurst; i++ {
		release, err := gate.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if _, err := gate.acquire(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("burst exhausted: %v", err)
	}
	now = now.Add(999 * time.Millisecond)
	if _, err := gate.acquire(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("early refill: %v", err)
	}
	now = now.Add(time.Millisecond)
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	now = now.Add(time.Hour)
	for i := 0; i < loginBurst; i++ {
		release, err := gate.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if _, err := gate.acquire(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("refill must be bounded: %v", err)
	}
}

func TestLoginAdmissionConcurrentAndCancellation(t *testing.T) {
	gate := newLoginAdmission(time.Now)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	var admitted sync.WaitGroup
	admitted.Add(32)
	done := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := gate.acquire(context.Background())
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrRateLimited) {
				t.Errorf("acquire: %v", err)
			}
			admitted.Done()
			if err == nil {
				<-done
				release()
			}
		}()
	}
	admitted.Wait()
	if n := accepted.Load(); n != loginConcurrency {
		t.Errorf("admitted %d, want %d", n, loginConcurrency)
	}
	close(done)
	wg.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gate.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire: %v", err)
	}
	release, err := gate.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestLoginInputBoundsBeforeDatabase(t *testing.T) {
	svc := NewService(nil)
	for _, input := range [][2]string{{strings.Repeat("a", 256), "password"}, {"alice", strings.Repeat("x", 1025)}} {
		_, _, err := svc.Login(context.Background(), input[0], input[1])
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("oversized input: %v", err)
		}
	}
	if _, err := HashPassword(strings.Repeat("x", 1025)); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("oversized new password: %v", err)
	}
}

func TestLoginRateLimitHTTP(t *testing.T) {
	gate := newLoginAdmission(time.Now)
	for i := 0; i < loginConcurrency; i++ {
		release, err := gate.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	h := &Handler{svc: &Service{logins: gate}}
	r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"alice","password":"password"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.login(w, r)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("response: %d %v", w.Code, w.Header())
	}
}

type canceledLoginConnector struct{ entered chan struct{} }

func (c canceledLoginConnector) Connect(ctx context.Context) (driver.Conn, error) {
	close(c.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func (c canceledLoginConnector) Driver() driver.Driver { return nil }

func TestCanceledLoginReleasesAdmission(t *testing.T) {
	connector := canceledLoginConnector{entered: make(chan struct{})}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	gate := newLoginAdmission(time.Now)
	svc := NewService(&database.DB{DB: pool})
	svc.logins = gate
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, _, err := svc.Login(ctx, "alice", "password"); result <- err }()
	<-connector.entered
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled login: %v", err)
	}
	for i := 0; i < loginConcurrency; i++ {
		release, err := gate.acquire(context.Background())
		if err != nil {
			t.Fatalf("leaked admission: %v", err)
		}
		defer release()
	}
}

func TestPasswordLimitPreservesNormalVerification(t *testing.T) {
	if !VerifyPassword(dummyHash, "cubeship-timing-equalizer") {
		t.Fatal("normal password rejected")
	}
	if VerifyPassword(dummyHash, strings.Repeat("x", MaxPasswordBytes+1)) {
		t.Fatal("oversized password accepted")
	}
}

func TestLoginBudgetsBelongToInstanceService(t *testing.T) {
	first, second := NewService(nil), NewService(nil)
	for i := 0; i < loginConcurrency; i++ {
		release, err := first.logins.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	if _, err := first.logins.acquire(context.Background()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("same instance must share admission: %v", err)
	}
	release, err := second.logins.acquire(context.Background())
	if err != nil {
		t.Fatalf("another instance inherited exhausted admission: %v", err)
	}
	release()
}
