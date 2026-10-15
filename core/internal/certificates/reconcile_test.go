package certificates

import "testing"

func servedApp(host, reference string, deployed bool) ServedHost {
	return ServedHost{Host: host, App: reference, Deployed: deployed}
}

// The comparison is the point of the page: a certificate is for a name,
// a name belongs to an app, and either side can exist without the other.
func TestWhatEachSideOfTheComparisonMeans(t *testing.T) {
	certs := []Certificate{
		{Host: "kept.example.com"},
		{Host: "gone.example.com"},
		{Host: "cubeship.example.com"},
	}
	served := []ServedHost{
		servedApp("kept.example.com", "web/production/kept", true),
		servedApp("waiting.example.com", "web/production/waiting", true),
		servedApp("added.example.com", "web/production/added", false),
		{Host: "cubeship.example.com", Instance: true, Deployed: true},
	}

	out, missing := reconcile(certs, served, true)

	byHost := map[string]Certificate{}
	for _, c := range out {
		byHost[c.Host] = c
	}
	if got := byHost["kept.example.com"]; got.App != "web/production/kept" || got.Orphan {
		t.Errorf("a certificate for a served name came out %+v", got)
	}
	if got := byHost["cubeship.example.com"]; !got.Instance || got.Orphan {
		t.Errorf("the instance's own certificate came out %+v", got)
	}
	// Nothing answers there any more. It stays valid and stays in the
	// file; the only thing wrong with it is that it was paid for.
	if got := byHost["gone.example.com"]; !got.Orphan || got.App != "" {
		t.Errorf("a certificate for a name nothing serves came out %+v", got)
	}

	reasons := map[string]Reason{}
	for _, m := range missing {
		reasons[m.Host] = m.Reason
	}
	if len(missing) != 2 {
		t.Fatalf("missing is %+v, want two entries", missing)
	}
	// A name Traefik knows about and has not got a certificate for.
	if reasons["waiting.example.com"] != ReasonPending {
		t.Errorf("waiting.example.com is %q", reasons["waiting.example.com"])
	}
	// A name added after the last deploy is one Traefik has never been
	// told about — a container keeps the labels it was created with.
	if reasons["added.example.com"] != ReasonNotDeployed {
		t.Errorf("added.example.com is %q", reasons["added.example.com"])
	}
}

// With no domain or no contact address Traefik is started with no
// resolver at all, so nothing is pending: the instance is the answer,
// not the name.
func TestWithoutTLSEveryNameHasTheSameReason(t *testing.T) {
	_, missing := reconcile(nil, []ServedHost{
		servedApp("a.example.com", "web/production/a", true),
		servedApp("b.example.com", "web/production/b", false),
	}, false)

	if len(missing) != 2 {
		t.Fatalf("missing is %+v", missing)
	}
	for _, m := range missing {
		if m.Reason != ReasonNoTLS {
			t.Errorf("%s is %q, want %q", m.Host, m.Reason, ReasonNoTLS)
		}
	}
}

// A certificate covering several names covers all of them: a name on a
// SAN is not missing a certificate.
func TestANameOnASANIsNotMissing(t *testing.T) {
	certs := []Certificate{{Host: "api.example.com", SANs: []string{"admin.example.com"}}}
	_, missing := reconcile(certs, []ServedHost{
		servedApp("api.example.com", "web/production/api", true),
		servedApp("admin.example.com", "web/production/api", true),
	}, true)

	if len(missing) != 0 {
		t.Errorf("missing is %+v, want none", missing)
	}
}

// The registry is routed by its container's labels, and a container
// keeps the labels it was created with — one made before the instance
// had a domain carries no router at all. Traefik has then never heard of
// the name, and "pending" would be a lie: nothing is coming.
func TestTheRegistrysNameIsOnlyServedIfItsContainerSaysSo(t *testing.T) {
	served := []ServedHost{
		{Host: "cubeship.example.com", Instance: true, Deployed: true},
		{Host: "registry.cubeship.example.com", Instance: true, Deployed: false},
	}

	_, missing := reconcile(nil, served, true)

	reasons := map[string]Reason{}
	for _, m := range missing {
		reasons[m.Host] = m.Reason
	}
	if reasons["cubeship.example.com"] != ReasonPending {
		t.Errorf("the daemon's own name is %q", reasons["cubeship.example.com"])
	}
	if reasons["registry.cubeship.example.com"] != ReasonNotDeployed {
		t.Errorf("the registry's name is %q, want %q",
			reasons["registry.cubeship.example.com"], ReasonNotDeployed)
	}
}
