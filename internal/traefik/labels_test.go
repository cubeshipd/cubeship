package traefik

import "testing"

func TestLabels(t *testing.T) {
	labels := Labels("myapp", "myapp.example.com", 8080)

	want := map[string]string{
		"traefik.enable":                                              "true",
		"traefik.http.routers.cubeship-myapp.rule":                    "Host(`myapp.example.com`)",
		"traefik.http.routers.cubeship-myapp.entrypoints":              "websecure",
		"traefik.http.routers.cubeship-myapp.tls.certresolver":         "letsencrypt",
		"traefik.http.services.cubeship-myapp.loadbalancer.server.port": "8080",
		"traefik.docker.network":                                       "cubeship",
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
