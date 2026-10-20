package dockerx

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// How many times PullImage tries a registry pull when the failure looks
// transient. The first attempt is included; two waits sit between the
// three tries.
const pullAttempts = 3

// Waits between transient pull failures. Short enough that a flaky GHCR
// layer copy does not turn a deploy into minutes of silence; long enough
// that a connection reset has somewhere to settle.
var pullRetryBackoff = []time.Duration{
	time.Second,
	2 * time.Second,
}

// isTransientPullError reports whether a failed pull is worth trying
// again. Auth refusals, missing manifests and cancelled contexts are
// not; TCP resets, EOFs mid-copy and timeouts are — those are what a
// VPS talking to GHCR over IPv6 hits intermittently, and a single
// attempt turns them into a failed deploy (and, for a template install,
// a full undo of everything that run created).
func isTransientPullError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || temporary(netErr)) {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, p := range permanentPullSubstrings {
		if strings.Contains(msg, p) {
			return false
		}
	}
	for _, t := range transientPullSubstrings {
		if strings.Contains(msg, t) {
			return true
		}
	}
	return false
}

// temporary is net.Error.Temporary without the deprecated-method
// warning — Go still exposes it, and dial failures that are temporary
// are exactly what we want to retry.
func temporary(err net.Error) bool {
	type temporaryError interface {
		Temporary() bool
	}
	t, ok := err.(temporaryError)
	return ok && t.Temporary()
}

// Substrings that mean the registry answered and the answer will not
// change on another try. Matched before the transient list so a
// "connection reset" buried in an unrelated message cannot override
// an auth refusal.
var permanentPullSubstrings = []string{
	"unauthorized",
	"authentication required",
	"access denied",
	"denied:",
	"manifest unknown",
	"not found",
	"no such image",
	"invalid reference",
	"repository name must be lowercase",
	"name unknown",
}

// Substrings that appear in Docker/containerd pull failures when the
// transport dropped mid-layer. "failed to copy" is containerd's wrapper
// around a blob download; alone it is almost always a network cut.
var transientPullSubstrings = []string{
	"connection reset by peer",
	"connection timed out",
	"i/o timeout",
	"tls handshake timeout",
	"broken pipe",
	"unexpected eof",
	"use of closed network connection",
	"server closed idle connection",
	"http2: client connection lost",
	"http2: stream closed",
	"failed to copy",
	"temporary failure in name resolution",
	"no route to host",
	"network is unreachable",
	"too many requests",
	"toomanyrequests",
	"rate limit",
}
