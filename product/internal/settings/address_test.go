package settings

import (
	"context"
	"errors"
	"testing"
)

// The bug this file exists for: an A record pointing at 172.18.0.2.
//
// The daemon is a container on a bridge network, so the address it finds
// on its own interface is the bridge's. Written into a zone it does not
// merely fail to help — it replaces whatever was resolving at that name
// with something nothing outside this host can reach, which is a domain
// that goes dark. Empty is the honest answer and every caller has to
// treat it as one.
func TestNoPrivateAddressIsEverOffered(t *testing.T) {
	refused := []string{
		"172.18.0.2",     // a Docker bridge, which is what started this
		"172.16.0.1",     // the rest of that range
		"10.4.2.9",       // a LAN
		"192.168.1.5",    // a dashboard opened on somebody's home network
		"100.100.0.1",    // carrier-grade NAT: routable-looking, reachable by nobody
		"127.0.0.1",      // loopback
		"169.254.10.1",   // link-local, from an interface that never came up
		"0.0.0.0",        // unspecified
		"::1",            // loopback again
		"fe80::1",        // link-local again
		"fd00::1",        // a unique local address
		"not-an-address", // and anything that is not one at all
		"",
	}
	for _, address := range refused {
		if got := routable(address); got != "" {
			t.Errorf("routable(%q) = %q, want empty", address, got)
		}
	}

	accepted := []string{"2.57.91.91", "203.0.113.5", "2606:4700::1111"}
	for _, address := range accepted {
		if routable(address) == "" {
			t.Errorf("routable(%q) refused a public address", address)
		}
	}
}

// The Host header is evidence about how this instance is reached from
// outside — but only when it is an address, and only when that address
// is one the outside could have used. A dashboard opened at
// 192.168.1.5:3000 says where somebody is sitting, not where the world
// finds this machine.
func TestTheAddressTheDashboardWasOpenedAtIsCheckedTheSameWay(t *testing.T) {
	cases := map[string]string{
		"203.0.113.5:3000":       "203.0.113.5",
		"203.0.113.5":            "203.0.113.5",
		"[2606:4700::1111]:3000": "2606:4700::1111",
		"192.168.1.5:3000":       "",
		"172.18.0.2":             "",
		"example.com":            "", // a name already resolves somewhere
		"":                       "",
	}
	for host, want := range cases {
		if got := ipOfHost(host); got != want {
			t.Errorf("ipOfHost(%q) = %q, want %q", host, got, want)
		}
	}
}

// What the operator typed is never filtered. They can see the machine,
// and an instance behind a split-horizon resolver may want an address
// nothing here would ever guess.
func TestWhatTheOperatorTypedWins(t *testing.T) {
	v := Values{PublicIP: "10.0.0.7"}
	if got := v.PublicAddressFor("203.0.113.5:3000"); got != "10.0.0.7" {
		t.Errorf("PublicAddressFor = %q, want the configured 10.0.0.7", got)
	}
	// And it is still what a running daemon answers, before anything is
	// asked of the host.
	s := &Service{host: fixedAddress("198.51.100.9")}
	if got := s.PublicIP(context.Background(), v, ""); got != "10.0.0.7" {
		t.Errorf("Service.PublicIP = %q, want the configured 10.0.0.7", got)
	}
}

// The host's own answer sits below the two kinds of evidence about how
// this instance is reached, and above the daemon's own interface —
// which in a container is the address this whole file is about.
func TestTheHostIsAskedWhenNothingElseKnows(t *testing.T) {
	ctx := context.Background()
	s := &Service{host: fixedAddress("198.51.100.9")}

	if got := s.PublicIP(ctx, Values{}, "example.com"); got != "198.51.100.9" {
		t.Errorf("PublicIP with only the host to go on = %q, want 198.51.100.9", got)
	}
	// The dashboard opened at an address outranks it: that is evidence
	// this instance really is reached there.
	if got := s.PublicIP(ctx, Values{}, "203.0.113.5:3000"); got != "203.0.113.5" {
		t.Errorf("PublicIP = %q, want the address the dashboard was opened at", got)
	}
	// A host that answers with a bridge address is a host that answered
	// nothing.
	private := &Service{host: fixedAddress("172.18.0.2")}
	if got := private.PublicIP(ctx, Values{}, "example.com"); got == "172.18.0.2" {
		t.Error("a private address from the host was offered as this instance's own")
	}
}

// `ip route get` prints the route and then the address a packet would
// leave from. Only the last of those is a fact about this machine.
func TestTheRouteSourceIsTheOnlyPartWorthReading(t *testing.T) {
	cases := map[string]string{
		"192.0.2.1 via 10.0.0.1 dev eth0 src 2.57.91.91 uid 0 \n    cache": "2.57.91.91",
		"192.0.2.1 dev eth0 src 203.0.113.5 uid 0":                         "203.0.113.5",
		"192.0.2.1 via 10.0.0.1 dev eth0 src 203.0.113.5 metric 100 uid 0": "203.0.113.5",
		"RTNETLINK answers: Network is unreachable":                        "",
		"":                                    "",
		"192.0.2.1 via 10.0.0.1 dev eth0 src": "", // truncated: nothing follows
	}
	for out, want := range cases {
		if got := RouteSource(out); got != want {
			t.Errorf("RouteSource(%q) = %q, want %q", out, got, want)
		}
	}
}

// Asking costs a container, and the dashboard reads the settings on
// several screens. The answer — including the empty one, from a host
// with no `ip` on it — is kept.
func TestTheHostIsAskedOnceAndThenRemembered(t *testing.T) {
	asked := 0
	address := RouteAddress(func(context.Context, ...string) (string, error) {
		asked++
		return "192.0.2.1 via 10.0.0.1 dev eth0 src 2.57.91.91 uid 0", nil
	})
	for range 5 {
		if got := address.Address(context.Background()); got != "2.57.91.91" {
			t.Fatalf("Address = %q", got)
		}
	}
	if asked != 1 {
		t.Errorf("the host was asked %d times, want 1", asked)
	}

	failing := 0
	nothing := RouteAddress(func(context.Context, ...string) (string, error) {
		failing++
		return "", errors.New("ip: not found")
	})
	for range 5 {
		if got := nothing.Address(context.Background()); got != "" {
			t.Errorf("Address = %q, want empty", got)
		}
	}
	if failing != 1 {
		t.Errorf("a host that cannot answer was asked %d times, want 1", failing)
	}
}

type fixedAddress string

func (f fixedAddress) Address(context.Context) string { return string(f) }
