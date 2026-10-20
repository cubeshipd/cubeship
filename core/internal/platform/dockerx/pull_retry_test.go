package dockerx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestIsTransientPullError(t *testing.T) {
	ghcrReset := errors.New(`pull image "ghcr.io/lucasaarch/jian-gateway:0.5.0": failed to copy: read tcp [2a02:c207:2358:5774::1]:42282->[2606:50c0:8000::154]:443: read: connection reset by peer`)

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "ghcr connection reset", err: ghcrReset, want: true},
		{name: "failed to copy alone", err: errors.New("failed to copy: unexpected EOF"), want: true},
		{name: "i/o timeout", err: errors.New("dial tcp: i/o timeout"), want: true},
		{name: "rate limit", err: errors.New("toomanyrequests: rate limit exceeded"), want: true},
		{name: "net timeout", err: fakeNetError{timeout: true}, want: true},
		{name: "manifest unknown", err: errors.New(`pull image "x": manifest unknown`), want: false},
		{name: "unauthorized", err: errors.New("unauthorized: authentication required"), want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "deadline", err: context.DeadlineExceeded, want: false},
		{name: "wrapped canceled", err: fmt.Errorf("pull: %w", context.Canceled), want: false},
		{name: "unrelated", err: errors.New("something else went wrong"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isTransientPullError(tc.err)
			if got != tc.want {
				t.Fatalf("isTransientPullError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type fakeNetError struct {
	timeout   bool
	temporary bool
}

func (e fakeNetError) Error() string   { return "fake net error" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.temporary }

var _ net.Error = fakeNetError{}

func TestPullImageRetriesTransientThenSucceeds(t *testing.T) {
	orig := pullRetryBackoff
	pullRetryBackoff = []time.Duration{0, 0}
	defer func() { pullRetryBackoff = orig }()

	fake := &fakeAPI{pullStreams: []string{
		`{"errorDetail":{"message":"failed to copy: read tcp: connection reset by peer"},"error":"failed to copy: read tcp: connection reset by peer"}` + "\n",
		`{"status":"Download complete"}` + "\n",
	}}
	c := newWithAPI(fake)

	if err := c.PullImage(context.Background(), "ghcr.io/acme/app:1", nil); err != nil {
		t.Fatalf("PullImage: %v", err)
	}
	if fake.pullCalls != 2 {
		t.Fatalf("expected 2 pull attempts, got %d", fake.pullCalls)
	}
}

func TestPullImageDoesNotRetryPermanent(t *testing.T) {
	fake := &fakeAPI{pullStream: `{"error":"manifest unknown"}` + "\n"}
	c := newWithAPI(fake)

	err := c.PullImage(context.Background(), "ghcr.io/acme/app:missing", nil)
	if err == nil {
		t.Fatal("expected manifest unknown to fail")
	}
	if fake.pullCalls != 1 {
		t.Fatalf("permanent errors must not retry, got %d calls", fake.pullCalls)
	}
}

func TestPullImageExhaustsTransientRetries(t *testing.T) {
	orig := pullRetryBackoff
	pullRetryBackoff = []time.Duration{0, 0}
	defer func() { pullRetryBackoff = orig }()

	stream := `{"error":"failed to copy: connection reset by peer"}` + "\n"
	fake := &fakeAPI{pullStreams: []string{stream, stream, stream}}
	c := newWithAPI(fake)

	err := c.PullImage(context.Background(), "ghcr.io/acme/app:1", nil)
	if err == nil {
		t.Fatal("expected the last reset to surface")
	}
	if fake.pullCalls != pullAttempts {
		t.Fatalf("expected %d attempts, got %d", pullAttempts, fake.pullCalls)
	}
	if !isTransientPullError(err) {
		t.Fatalf("expected a transient error back, got %v", err)
	}
}
