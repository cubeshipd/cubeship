package extregistry

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"cubeship/internal/settings"
)

var errPrivateRegistry = errors.New("an unregistered registry must use a public address")

// Go's global-unicast check still admits reserved and translation networks.
var registryReserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

// Control sees the resolved socket address, so redirects and DNS changes cannot bypass the check.
func publicRegistrySocket(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errPrivateRegistry
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return errPrivateRegistry
	}
	ip = ip.Unmap()
	if netip.MustParsePrefix("64:ff9b::/96").Contains(ip) {
		bytes := ip.As16()
		ip = netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]})
	}
	if !settings.Routable(net.IP(ip.AsSlice())) {
		return errPrivateRegistry
	}
	for _, prefix := range registryReserved {
		if prefix.Contains(ip) {
			return errPrivateRegistry
		}
	}
	return nil
}

func newPublicRegistryClient() *http.Client {
	return publicRegistryClient(nil)
}

func tokenRealmClient(base *http.Client, registryHost string, realm *url.URL) *http.Client {
	registryName, registryPort, err := net.SplitHostPort(registryHost)
	if err != nil {
		registryName, registryPort = registryHost, "443"
	}
	if realmPort := realm.Port(); realmPort != "" {
		if strings.EqualFold(registryName, realm.Hostname()) && registryPort == realmPort {
			return sameHostTokenClient(base, realm)
		}
	} else if registryPort == "443" && strings.EqualFold(registryName, realm.Hostname()) {
		return sameHostTokenClient(base, realm)
	}
	return publicRegistryClient(base)
}

func sameHostTokenClient(base *http.Client, realm *url.URL) *http.Client {
	client := *base
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !strings.EqualFold(req.URL.Host, realm.Host) {
			return errors.New("the registry token redirect must stay on the token server over HTTPS")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after ten registry token redirects")
		}
		return nil
	}
	return &client
}

func publicRegistryClient(base *http.Client) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if base != nil {
		if original, ok := base.Transport.(*http.Transport); ok {
			transport = original.Clone()
		}
	}
	// An environment proxy would resolve the destination outside this guard.
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: publicRegistrySocket}).DialContext
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || req.URL.Scheme != "https" {
				return errors.New("the registry redirect must stay on HTTPS and within ten requests")
			}
			return nil
		},
	}
	if base != nil {
		client.Timeout = base.Timeout
		client.Jar = base.Jar
	}
	return client
}
