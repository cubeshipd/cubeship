package credential

import (
	"strings"
	"testing"
)

// The sentence that refuses a delete has to name what is in the way.
// "In use" that does not say by what is a refusal somebody has to go
// hunting to satisfy.
func TestTheRefusalNamesWhatIsUsingIt(t *testing.T) {
	err := InUseError([]Use{
		{Kind: "registry", Name: "ghcr.io"},
		{Kind: "DNS provider", Name: "Cloudflare"},
	})
	for _, want := range []string{"ghcr.io", "Cloudflare"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not name %q", err, want)
		}
	}
	// A use with nothing to distinguish it reads as itself, not with a
	// trailing space where a name would have been.
	if got := (Use{Kind: "the instance's own DNS"}).String(); got != "the instance's own DNS" {
		t.Errorf("Use.String() = %q", got)
	}
}
