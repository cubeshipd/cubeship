package template

import (
	"slices"
	"testing"
)

func codesOf(ds []Diagnostic) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		if d.Severity == Error {
			out = append(out, d.Code)
		}
	}
	return out
}

func TestWhatAVolumeIsHeldTo(t *testing.T) {
	const head = "version: 1\nproject: q\n"
	for _, tc := range []struct {
		name string
		file string
		want []string
	}{
		{"a volume on a pinned template", head + `minCubeship: "0.7.0"
apps:
  - key: broker
    image: rabbitmq
    volumes:
      - path: /var/lib/rabbitmq/
`, nil},
		{"no minCubeship", head + `apps:
  - key: broker
    image: rabbitmq
    volumes:
      - path: /data
`, []string{"volume.min-cubeship"}},
		{"a minCubeship older instances satisfy", head + `minCubeship: ">=0.6.0"
apps:
  - key: broker
    image: rabbitmq
    volumes:
      - path: /data
`, []string{"volume.min-cubeship"}},
		{"two copies", head + `minCubeship: "0.7.0"
apps:
  - key: broker
    image: rabbitmq
    scale: 2
    volumes:
      - path: /data
`, []string{"volume.one-copy"}},
		{"a bad and a repeated path", head + `minCubeship: "0.7.0"
apps:
  - key: broker
    image: rabbitmq
    volumes:
      - path: /proc/x
      - path: /data
      - path: /data/
`, []string{"volume.path", "volume.duplicate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := codesOf(Validate([]byte(tc.file)).Diagnostics)
			if !slices.Equal(got, tc.want) {
				t.Errorf("errors %v, want %v", got, tc.want)
			}
		})
	}

	r := Validate([]byte(head + `minCubeship: "0.7.0"
apps:
  - key: broker
    image: rabbitmq
    volumes:
      - path: /var/lib/rabbitmq/
`))
	if !r.OK || len(r.Manifest.Apps[0].Volumes) != 1 || r.Manifest.Apps[0].Volumes[0].Path != "/var/lib/rabbitmq" {
		t.Errorf("normalized volumes = %+v", r.Manifest)
	}
}
