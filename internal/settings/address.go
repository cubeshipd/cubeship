package settings

import (
	"net"
	"net/http"
	"strings"
)

// PublicAddressFor is what this instance's DNS records should point at.
//
// Three answers, in order of how much they can be trusted.
//
// **What the operator typed** wins. They can see the machine.
//
// **The address the request arrived at**, when it is an IP literal. On a
// fresh install that is exactly right and free: the dashboard is reached
// at `http://<ip>:3000` before there is any domain, so the address in
// the URL bar is, by construction, an address that reaches this host
// from where the operator is sitting. Once there is a domain it is a
// name rather than an address, and this stops answering.
//
// **The interface**, last, and it is usually wrong: the daemon runs as a
// container on a bridge network, so what it reads there is the
// container's own 172.x address. It is kept because a daemon on the host
// answers correctly, and something is better than an empty field.
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
	host = strings.Trim(host, "[]")

	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// ReachedAt reads the address a request arrived at, preferring what a
// proxy recorded over what it rewrote.
func ReachedAt(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return r.Host
}

// outboundAddress is the address on the interface this host reaches the
// internet through.
//
// The UDP dial sends nothing: connecting a datagram socket only makes
// the kernel choose a route and bind a local address, which is the
// question being asked. The address it names is never contacted.
func outboundAddress() string {
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
