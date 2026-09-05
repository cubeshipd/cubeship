package traefik

import "testing"

func TestLabels(t *testing.T) {
	labels := Labels("myapp", []Domain{{Host: "myapp.example.com", Port: 8080}}, true)

	want := map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.cubeship-myapp.rule":                      "Host(`myapp.example.com`)",
		"traefik.http.routers.cubeship-myapp.entrypoints":               "websecure",
		"traefik.http.routers.cubeship-myapp.service":                   "cubeship-myapp",
		"traefik.http.routers.cubeship-myapp.tls.certresolver":          "letsencrypt",
		"traefik.http.services.cubeship-myapp.loadbalancer.server.port": "8080",
		"traefik.docker.network":                                        "cubeship",
	}

	for k, v := range want {
		if labels[k] != v {
			t.Errorf("label %q: got %q, want %q", k, labels[k], v)
		}
	}
	if len(labels) != len(want) {
		t.Errorf("got %d labels, want %d: %v", len(labels), len(want), labels)
	}
}

// Until a contact address is configured there is no certificate
// resolver, so the router has to sit on plain HTTP. Asking for a
// certificate that cannot be issued would make the app unreachable
// rather than merely unencrypted.
func TestLabelsWithoutTLSServeOverHTTP(t *testing.T) {
	labels := Labels("myapp", []Domain{{Host: "myapp.example.com", Port: 8080}}, false)

	if got := labels["traefik.http.routers.cubeship-myapp.entrypoints"]; got != "web" {
		t.Errorf("entrypoint is %q, want web", got)
	}
	if _, present := labels["traefik.http.routers.cubeship-myapp.tls.certresolver"]; present {
		t.Error("a certificate resolver was requested with no resolver configured")
	}
	if labels["traefik.http.routers.cubeship-myapp.rule"] != "Host(`myapp.example.com`)" {
		t.Errorf("the host rule changed: %v", labels)
	}
}

// Each name gets its own router *and its own service*, because each
// names its own port. A single router listing both hosts would have one
// service behind it, so whichever port that service named would answer
// for both names — and the second name would silently serve the wrong
// thing.
func TestEachDomainGetsItsOwnPort(t *testing.T) {
	labels := Labels("myapp", []Domain{
		{Host: "api.example.com", Port: 8080},
		{Host: "admin.example.com", Port: 9000},
	}, true)

	for label, want := range map[string]string{
		"traefik.http.routers.cubeship-myapp.rule":                        "Host(`api.example.com`)",
		"traefik.http.services.cubeship-myapp.loadbalancer.server.port":   "8080",
		"traefik.http.routers.cubeship-myapp-1.rule":                      "Host(`admin.example.com`)",
		"traefik.http.services.cubeship-myapp-1.loadbalancer.server.port": "9000",
		"traefik.http.routers.cubeship-myapp-1.service":                   "cubeship-myapp-1",
	} {
		if labels[label] != want {
			t.Errorf("label %q: got %q, want %q", label, labels[label], want)
		}
	}
}

// An app with no domain is not something Traefik should have an opinion
// about. Enabling it with no rule would leave a container Traefik knows
// and cannot route.
func TestNoDomainsMeansNoRouting(t *testing.T) {
	labels := Labels("myapp", nil, true)

	if _, present := labels["traefik.enable"]; present {
		t.Errorf("Traefik was enabled for a container with nothing to route: %v", labels)
	}
}
