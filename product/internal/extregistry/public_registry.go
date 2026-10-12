package extregistry

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

	"cubeship/internal/settings"
)

var errPrivateRegistry = errors.New("an unregistered registry must use a public address")

// Control sees the resolved socket address, so redirects and DNS changes cannot bypass the check.
func publicRegistrySocket(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !settings.Routable(net.ParseIP(host)) {
		return errPrivateRegistry
	}
	return nil
}

func newPublicRegistryClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// An environment proxy would resolve the destination outside this guard.
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: publicRegistrySocket}).DialContext
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 || req.URL.Scheme != "https" {
				return errors.New("the registry redirect must stay on HTTPS and within ten requests")
			}
			return nil
		},
	}
}
