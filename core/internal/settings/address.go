package settings

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PublicAddressFor is what this instance's DNS records should point at.
//
// Three answers, in order of how much they can be trusted.
//
// **What the operator typed** wins, and it is the only one that is not
// filtered below: they can see the machine, and an instance served
// behind a split-horizon resolver may genuinely want an address nothing
// here would guess.
//
// **The address the request arrived at**, when it is an IP literal. On a
// fresh install that is exactly right and free: the dashboard is reached
// at `http://<ip>:3000` before there is any domain, so the address in
// the URL bar is, by construction, an address that reaches this host
// from where the operator is sitting. Once there is a domain it is a
// name rather than an address, and this stops answering.
//
// **The daemon's own interface**, last, and only when the daemon is a
// host process — which is `make dev`. In a container it reads the
// container's bridge address, and that is filtered out below rather
// than offered.
//
// Service.PublicIP adds a fourth between the last two: the *host's*
// address, asked of the host itself. It is not here because this is a
// pure function over stored values and that answer takes a command.
//
// **A private address is never an answer.** This value is written into
// public DNS, and 172.18.0.2 in an A record is not a worse guess than
// nothing — it is a name that stops resolving, at whatever it was
// pointed at before. Empty is the honest answer, and every caller has to
// treat it as one.
//
// This is deliberately not asked of an outside service. Cubeship depends
// on nothing beyond Docker, and "what is my address" is not worth being
// the exception — especially for a product someone self-hosts to avoid
// exactly that.
func (v Values) PublicAddressFor(reachedAt string) string {
	if configured := strings.TrimSpace(v.Get(PublicIP)); configured != "" {
		return configured
	}
	if ip := ipOfHost(reachedAt); ip != "" {
		return ip
	}
	return outboundAddress()
}

// Routable reports whether an address is one the internet could reach —
// which is the only kind worth writing into a record.
//
// Everything else is refused by name rather than by a single "is it
// private" test, because they arrive from different places and each one
// has been somebody's broken domain: a bridge address from a daemon in a
// container, a LAN address from a dashboard opened at 192.168.1.5, a
// carrier-grade address from a host behind a provider's NAT, a
// link-local one from an interface that never came up.
func Routable(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// 100.64.0.0/10, which is not "private" to Go and is not reachable
	// from anywhere either: it is what a provider hands a host that sits
	// behind their own NAT.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// routable parses and checks in one step, for a value that arrived as a
// string.
func routable(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if !Routable(ip) {
		return ""
	}
	return ip.String()
}

// ipOfHost reads a Host header for an IP literal, and answers "" for a
// name. A name in that header is a name that already resolves somewhere,
// which is not what a record about to be written needs.
func ipOfHost(host string) string {
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return routable(strings.Trim(host, "[]"))
}

// ReachedAt reads the address a request arrived at, preferring what a
// proxy recorded over what it rewrote.
func ReachedAt(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return r.Host
}

// outboundAddress is the address on the interface *this process* reaches
// the internet through — the host's when the daemon is a host process,
// and a bridge address when it is a container. Only the first is ever
// returned; see Routable.
//
// The UDP dial sends nothing: connecting a datagram socket only makes
// the kernel choose a route and bind a local address, which is the
// question being asked. The address it names is never contacted.
func outboundAddress() string {
	conn, err := net.Dial("udp4", routeProbe+":9")
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return routable(addr.IP.String())
}

// routeProbe is the address every route lookup here is made against. It
// is TEST-NET-1, which is reserved for documentation and routed
// nowhere: the question is which interface the kernel would use, and
// nothing is ever sent.
const routeProbe = "192.0.2.1"

// HostAddress answers what address this *machine* has on the interface
// it reaches the internet through.
//
// It exists because the daemon usually cannot answer that about itself:
// it is a container on a bridge network, so its own outbound address is
// a 172.x one that belongs in nobody's DNS. The machinery to ask the
// host is the same one the firewall uses — a command run in the host's
// namespaces — and it lives outside this package for the same reason
// the firewall's does: this module knows what the answer means, not how
// to get it.
//
// Nil is a legal state. A daemon that cannot reach the host has one
// fewer answer, not a broken one.
type HostAddress interface {
	Address(ctx context.Context) string
}

// RouteAddress builds a HostAddress from something that can run a
// command on the host.
//
// `ip route get` rather than a socket, because a socket opened here is
// opened in the daemon's own network namespace whatever else it enters.
// It consults the routing table and sends nothing.
func RouteAddress(run func(ctx context.Context, argv ...string) (string, error)) HostAddress {
	return &routeAddress{run: run}
}

// HostAddressTTL is how long an answer is kept.
//
// Asking costs a container: reaching the host means starting one in its
// namespaces, and the dashboard reads the settings on several screens.
// A machine's own address changes about never, so this is long enough to
// make the cost invisible and short enough that an operator who did
// change it is not stuck with the old answer until a restart.
const HostAddressTTL = 10 * time.Minute

type routeAddress struct {
	run func(ctx context.Context, argv ...string) (string, error)

	mu      sync.Mutex
	address string
	asked   time.Time
}

func (r *routeAddress) Address(ctx context.Context) string {
	if r.run == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.asked.IsZero() && time.Since(r.asked) < HostAddressTTL {
		return r.address
	}
	// The empty answer is cached too. A host with no `ip` on it answers
	// nothing every time, and paying a container per settings read to
	// find that out again is worse than being briefly wrong.
	r.asked = time.Now()
	out, err := r.run(ctx, "ip", "-4", "route", "get", routeProbe)
	if err != nil {
		r.address = ""
		return ""
	}
	r.address = routable(RouteSource(out))
	return r.address
}

// RouteSource reads the address `ip route get` says a packet would leave
// from:
//
//	192.0.2.1 via 10.0.0.1 dev eth0 src 203.0.113.5 uid 0
//
// The interesting word is the one after "src". Everything else on the
// line is about the route rather than about this host.
func RouteSource(out string) string {
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "src" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}
