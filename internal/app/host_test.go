package app

import "testing"

// A name goes into a Traefik rule — Host(`%s`) — so what it may contain
// is the DNS grammar and nothing wider. Anything else is a routing rule
// somebody else wrote, or a typo that becomes one.
func TestWhatCanBeAHostName(t *testing.T) {
	for host, want := range map[string]bool{
		"myapp.example.com":     true,
		"api-v2.example.com":    true,
		"internal":              true,
		"a.b.c.d.example.com":   true,
		"0800.example.com":      true,
		"":                      false,
		"-leading.example.com":  false,
		"trailing-.example.com": false,
		"two..dots.example":     false,
		".example.com":          false,
		"under_score.example":   false,
		"space in.example.com":  false,
		// The reason this exists at all: the rule is a template, and
		// these close it and write another one.
		"a`)||Host(`anything.example.com": false,
		"example.com`, `evil.example":     false,
	} {
		if got := ValidHost(host); got != want {
			t.Errorf("ValidHost(%q) = %v, want %v", host, got, want)
		}
	}

	long := ""
	for len(long) < MaxHostLength {
		long += "label."
	}
	if ValidHost(long + "example.com") {
		t.Error("a name longer than a DNS name can be was accepted")
	}
}

// Giving an app an address used to mean owning a domain, pointing a
// record at this host and waiting for it. The instance already has a
// domain — an sslip.io address, on a default install — and every name
// under one of those resolves here already, so the app's own reference
// under it is a name that works the moment it is added.
func TestTheSuggestedHostIsTheReferenceUnderTheInstancesDomain(t *testing.T) {
	ref := Reference{Project: "octokube", Environment: "production", Name: "octokube-server"}

	for domain, want := range map[string]string{
		"75-119-141-235.sslip.io": "octokube-server.production.octokube.75-119-141-235.sslip.io",
		"Example.COM":             "octokube-server.production.octokube.example.com",
		"example.com.":            "octokube-server.production.octokube.example.com",
		// Nothing to build it under, so there is nothing to suggest.
		"": "",
		// A domain that could not be a host name would make one that
		// Traefik could not route.
		"not a domain": "",
	} {
		if got := SuggestedHostFor(ref, domain); got != want {
			t.Errorf("SuggestedHostFor(%q) = %q, want %q", domain, got, want)
		}
	}
}
