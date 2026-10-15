package selfdial

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

const own = "203.0.113.7"

func dialer(dialed *[]string, proxyUp bool) *Dialer {
	return &Dialer{
		Self:  func(context.Context) []string { return []string{own} },
		Proxy: "cubeship-traefik",
		Lookup: func(_ context.Context, host string) ([]string, error) {
			switch host {
			case "cubeship.dev":
				return []string{"2001:db8::1", own}, nil
			case "raw.githubusercontent.com":
				return []string{"185.199.108.133"}, nil
			}
			return nil, errors.New("no such host")
		},
		Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			*dialed = append(*dialed, address)
			if !proxyUp && address == "cubeship-traefik:443" {
				return nil, errors.New("no such host")
			}
			client, server := net.Pipe()
			server.Close()
			return client, nil
		},
	}
}

func TestWhereAConnectionGoes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address string
		proxyUp bool
		want    []string
	}{
		{"a name that resolves here goes to the proxy", "cubeship.dev:443", true, []string{"cubeship-traefik:443"}},
		{"the port is kept", "cubeship.dev:80", true, []string{"cubeship-traefik:80"}},
		{"this machine's own address, typed", own + ":443", true, []string{"cubeship-traefik:443"}},
		{"a name elsewhere leaves the machine", "raw.githubusercontent.com:443", true, []string{"raw.githubusercontent.com:443"}},
		{"a name that does not resolve is dialled as it was", "nowhere.invalid:443", true, []string{"nowhere.invalid:443"}},
		{"with no proxy to reach, the public route", "cubeship.dev:443", false, []string{"cubeship-traefik:443", "cubeship.dev:443"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dialed []string
			conn, err := dialer(&dialed, tc.proxyUp).DialContext(context.Background(), "tcp", tc.address)
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			if !slices.Equal(dialed, tc.want) {
				t.Fatalf("dialled %v, want %v", dialed, tc.want)
			}
		})
	}
}

// The point of keeping Host and SNI: the proxy is reached by another
// address, and TLS still verifies against the name that was asked for.
func TestTLSVerifiesAgainstTheNameAskedFor(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Host)
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())

	client := Client(&Dialer{
		Self:  func(context.Context) []string { return []string{own} },
		Proxy: "127.0.0.1",
		Lookup: func(context.Context, string) ([]string, error) {
			return []string{own}, nil
		},
	}, 5*time.Second)
	client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig

	// httptest's certificate is issued for example.com.
	resp, err := client.Get("https://example.com:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "example.com:"+port {
		t.Fatalf("the proxy was asked for %q", body)
	}
}

func TestAnInstanceWithNoAddressDialsAsItWas(t *testing.T) {
	var dialed []string
	d := dialer(&dialed, true)
	d.Self = func(context.Context) []string { return nil }
	conn, err := d.DialContext(context.Background(), "tcp", "cubeship.dev:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if !slices.Equal(dialed, []string{"cubeship.dev:443"}) {
		t.Fatalf("dialled %v", dialed)
	}
}
