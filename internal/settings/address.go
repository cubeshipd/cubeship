package settings

import (
	"net"
	"strings"
)

// PublicAddress is the address the instance's DNS records should point
// at: the address on the interface this host reaches the internet
// through.
//
// It is worked out locally rather than asked of an external service.
// Cubeship depends on nothing beyond Docker, and "what is my public IP"
// is the one question where reaching out is easy to reach for — but a
// VPS holds its public address on its own interface, so the answer is
// already here.
//
// The UDP dial sends nothing. Connecting a datagram socket only makes
// the kernel choose a route and bind a local address, which is exactly
// the question being asked; the address it names is never contacted.
//
// It is a suggestion, not a fact. A host behind NAT answers with its
// private address, and that is why the setting can be overridden — an
// operator who knows better types it, and what they typed wins.
func PublicAddress() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return ""
	}
	return addr.IP.String()
}

// PublicAddressFor returns what records should point at: the operator's
// answer if they gave one, and this host's own otherwise.
func (v Values) PublicAddressFor() string {
	if configured := strings.TrimSpace(v.Get(PublicIP)); configured != "" {
		return configured
	}
	return PublicAddress()
}
