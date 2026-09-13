// Package selfdial is an HTTP client that reaches a name served by this
// instance without leaving the machine.
//
// A request from inside the box for a name whose record points back at
// it leaves for the machine's own public address, and a host that does
// not hairpin its own NAT answers nothing: the connection hangs until the
// client gives up. The catalog at cubeship.dev running as an app on the
// same instance that reads it is exactly that.
//
// Traefik holds ports 80 and 443 on this machine, so anything that
// resolves here is served by it. When a name resolves to one of this
// instance's own addresses, the connection goes to Traefik over the
// shared network instead — same port, same Host, same SNI — and TLS
// verifies against the certificate Traefik serves for that name, as it
// would from outside.
package selfdial

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

// Dialer decides where a connection goes.
type Dialer struct {
	// Self is every address that reaches this instance from outside:
	// the public address it knows, and what its own domain resolves to —
	// which is the only answer on a machine behind 1:1 NAT, where the
	// public address is on nobody's interface.
	Self func(ctx context.Context) []string
	// Proxy is the host a connection to Self is sent to instead: Traefik's
	// container name on the shared network.
	Proxy string

	// Lookup and Dial are replaceable for a test. Nil is the system's.
	Lookup func(ctx context.Context, host string) ([]string, error)
	Dial   func(ctx context.Context, network, address string) (net.Conn, error)

	mu      sync.Mutex
	self    []string
	checked time.Time
}

// selfTTL is how long the instance's own addresses are remembered.
// Working them out reads the settings, may ask the host and resolves a
// name, and none of that changes between two requests.
const selfTTL = 5 * time.Minute

// DialContext is an http.Transport's DialContext.
func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || d.Proxy == "" {
		return d.dial(ctx, network, address)
	}
	if d.resolvesHere(ctx, host) {
		conn, err := d.dial(ctx, network, net.JoinHostPort(d.Proxy, port))
		if err == nil {
			return conn, nil
		}
		// A daemon running on the host rather than on the shared network
		// — `make dev` — cannot resolve the proxy's name. The public
		// route is what it had before this existed.
	}
	return d.dial(ctx, network, address)
}

func (d *Dialer) own(ctx context.Context) []string {
	if d.Self == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.checked.IsZero() || time.Since(d.checked) > selfTTL {
		d.self, d.checked = d.Self(ctx), time.Now()
	}
	return d.self
}

func (d *Dialer) resolvesHere(ctx context.Context, host string) bool {
	own := d.own(ctx)
	if len(own) == 0 {
		return false
	}
	addrs := []string{host}
	if net.ParseIP(host) == nil {
		found, err := d.lookup(ctx, host)
		if err != nil {
			return false
		}
		addrs = found
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		for _, o := range own {
			if ip != nil && ip.Equal(net.ParseIP(o)) {
				return true
			}
		}
	}
	return false
}

func (d *Dialer) lookup(ctx context.Context, host string) ([]string, error) {
	if d.Lookup != nil {
		return d.Lookup(ctx, host)
	}
	return net.DefaultResolver.LookupHost(ctx, host)
}

func (d *Dialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if d.Dial != nil {
		return d.Dial(ctx, network, address)
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

// Client is an http.Client whose connections go through d.
func Client(d *Dialer, timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = d.DialContext
	return &http.Client{Timeout: timeout, Transport: transport}
}
