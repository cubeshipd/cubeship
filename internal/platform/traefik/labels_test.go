package traefik

import "testing"

func TestLabels(t *testing.T) {
	labels := Labels("myapp", "myapp.example.com", 8080, true)

	want := map[string]string{
		"traefik.enable": "true",
		"traefik.http.routers.cubeship-myapp.rule":                      "Host(`myapp.example.com`)",
		"traefik.http.routers.cubeship-myapp.entrypoints":               "websecure",
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
	labels := Labels("myapp", "myapp.example.com", 8080, false)

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
