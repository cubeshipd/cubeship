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
