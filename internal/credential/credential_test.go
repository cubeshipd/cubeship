package credential

import (
	"strings"
	"testing"
)

// A provider offered has to be one the daemon can act through. Without
// this, adding a name to Providers() would compile, appear in the
// create form, and store a secret for a job nothing can do with it.
func TestEveryProviderHasASpec(t *testing.T) {
	for _, p := range Providers() {
		sp, ok := specs[p]
		if !ok {
			t.Errorf("%s is offered but has no spec", p)
			continue
		}
		if sp.name == "" || sp.passwordLabel == "" || sp.hint == "" {
			t.Errorf("%s has an incomplete spec: %+v", p, sp)
		}
		if len(sp.capabilities) == 0 {
			t.Errorf("%s can be used for nothing, so storing one would be storing a secret for no job", p)
		}
		for _, c := range sp.capabilities {
			if !known(c) {
				t.Errorf("%s claims %q, which is not a capability this release defines", p, c)
			}
		}
		if !p.Valid() {
			t.Errorf("%s is offered but Valid says no", p)
		}
	}
	for p := range specs {
		found := false
		for _, offered := range Providers() {
			if offered == p {
				found = true
			}
		}
		if !found {
			t.Errorf("%s has a spec but is not offered, so nobody can create one", p)
		}
	}
}

// The case the whole module exists for: one AWS access key reaches
// Route 53 and ECR, and before this it was stored twice — once as a
// "route53" DNS provider and once as an "aws" registry login.
func TestOneAWSKeyDoesDNSAndRegistries(t *testing.T) {
	if !ProviderAWS.Can(CapabilityDNS) {
		t.Error("an AWS key cannot do DNS, which is Route 53's whole point here")
	}
	if !ProviderAWS.Can(CapabilityRegistry) {
		t.Error("an AWS key cannot do registries, which is ECR's")
	}
}

// A capability the daemon cannot act on is a credential somebody would
// store for a job it can never do. Cloudflare has no registry; the
// DigitalOcean token could do DNS but this release has no client for
// it, and claiming it would be a promise nothing keeps.
func TestNobodyClaimsWhatTheDaemonCannotDo(t *testing.T) {
	if ProviderCloudflare.Can(CapabilityRegistry) {
		t.Error("cloudflare claims registries")
	}
	if ProviderGeneric.Can(CapabilityDNS) {
		t.Error("a registry login claims DNS")
	}
	if ProviderDigitalOcean.Can(CapabilityDNS) {
		t.Error("digitalocean claims DNS, and there is no DigitalOcean DNS client in this release")
	}

	// And the refusal says what the provider does instead — a refusal
	// that only says no is one somebody has to go and look up.
	err := CannotError(ProviderCloudflare, CapabilityRegistry)
	for _, want := range []string{"Cloudflare", "dns", "registry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q", err, want)
		}
	}
}

// A token is one value. A field asking for a name to go with it would
// be a field with no right answer, and one silently dropped would be a
// credential different from what somebody thought they stored.
func TestOnlyTheProvidersWithTwoHalvesAskForTwo(t *testing.T) {
	if !ProviderAWS.NeedsUsername() {
		t.Error("an AWS key is an id and a secret; both are needed")
	}
	if !ProviderGeneric.NeedsUsername() {
		t.Error("a registry login has a username")
	}
	for _, p := range []Provider{ProviderCloudflare, ProviderDigitalOcean} {
		if p.NeedsUsername() {
			t.Errorf("%s's secret is a single token, so there is no name to ask for", p)
		}
		if p.UsernameLabel() != "" {
			t.Errorf("%s names a first field it does not have", p)
		}
	}
}

// The sentence that refuses a delete has to name what is in the way.
// "In use" that does not say by what is a refusal somebody has to go
// hunting to satisfy.
func TestTheRefusalNamesWhatIsUsingIt(t *testing.T) {
	err := InUseError([]Use{
		{Kind: "registry", Name: "ghcr.io"},
		{Kind: "the instance's own DNS"},
	})
	for _, want := range []string{"ghcr.io", "the instance's own DNS"} {
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
