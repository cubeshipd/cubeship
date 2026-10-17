package template

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishedTemplatesExposeTheirExternalProtocols(t *testing.T) {
	tests := []struct {
		name string
		file string
		app  string
		port int
	}{
		{name: "rabbitmq AMQP", file: "cubeship-rabbitmq-template", app: "broker", port: 5672},
		{name: "mailpit SMTP", file: "cubeship-mailpit-template", app: "web", port: 1025},
		{name: "logstash Beats", file: "cubeship-logstash-template", app: "pipeline", port: 5044},
		{name: "signoz OTLP gRPC", file: "cubeship-signoz-template", app: "collector", port: 4317},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("testdata", "published", tt.file+".yaml")
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result := Validate(contents)
			if !result.OK {
				t.Fatalf("template is invalid: %+v", result.Diagnostics)
			}

			var app *NormalizedApp
			for i := range result.Manifest.Apps {
				if result.Manifest.Apps[i].Key == tt.app {
					app = &result.Manifest.Apps[i]
					break
				}
			}
			if app == nil {
				t.Fatalf("app %q not found", tt.app)
			}
			if len(app.TCP) != 1 || app.TCP[0].Port != tt.port {
				t.Fatalf("TCP ports = %+v, want container port %d", app.TCP, tt.port)
			}
		})
	}
}
