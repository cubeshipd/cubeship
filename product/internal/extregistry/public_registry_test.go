package extregistry

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func TestPublicRegistrySocketRefusesInternalDestinations(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "0.0.0.0", "0.1.2.3", "10.2.3.4", "172.16.1.2", "192.168.1.2", "169.254.169.254", "100.64.0.1", "198.18.0.1", "240.0.0.1", "::1", "::", "fc00::1", "fec0::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:0.1.2.3", "64:ff9b::7f00:1", "2002:7f00:1::", "224.0.0.1"} {
		if err := publicRegistrySocket("tcp", net.JoinHostPort(ip, "443"), nil); !errors.Is(err, errPrivateRegistry) {
			t.Errorf("internal destination %s was allowed: %v", ip, err)
		}
	}
	for _, ip := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111", "64:ff9b::808:808"} {
		if err := publicRegistrySocket("tcp", net.JoinHostPort(ip, "443"), nil); err != nil {
			t.Errorf("public destination %s was refused: %v", ip, err)
		}
	}
}

func TestAnonymousRegistryClientGuardsTheActualSocket(t *testing.T) {
	client := newPublicRegistryClient()
	transport := client.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("a proxy could bypass destination validation")
	}
	_, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443")
	if !errors.Is(err, errPrivateRegistry) {
		t.Fatalf("dial did not enforce the guard: %v", err)
	}
	for _, target := range []string{"http://example.com/token", "https://example.com/token"} {
		u, _ := url.Parse(target)
		err := client.CheckRedirect(&http.Request{URL: u}, nil)
		if (err == nil) != (u.Scheme == "https") {
			t.Fatalf("wrong redirect decision for %s: %v", target, err)
		}
	}
}

func TestTokenRealmCannotDowngradeOrEmbedCredentials(t *testing.T) {
	client := &http.Client{Transport: packageTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("an unsafe token realm reached the transport")
		return nil, nil
	})}
	v := newV2Client(client, &Credential{Host: "registry.example.com", Username: "user", Password: "secret"})
	for _, realm := range []string{"http://example.com/token", "https://user:secret@example.com/token", "/token"} {
		if _, err := v.mintToken(context.Background(), realm, "", ""); err == nil {
			t.Errorf("unsafe token realm was accepted: %s", realm)
		}
	}
}
