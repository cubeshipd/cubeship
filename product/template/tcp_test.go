package template

import (
	"slices"
	"testing"
)

func TestWhatATCPPortIsHeldTo(t *testing.T) {
	const head = "version: 1\nproject: q\n"
	const input = `inputs:
  - key: sshPort
    type: number
    label: SSH port
    default: 2222
    min: 1024
    max: 65535
`
	for _, tc := range []struct {
		name string
		file string
		want []string
	}{
		{"a port answered by an input", head + `minCubeship: "0.7.2"
` + input + `apps:
  - key: git
    image: gitlab/gitlab-ce
    tcp:
      - port: 22
        host: ${input.sshPort}
`, nil},
		{"a port the instance picks", head + `minCubeship: "0.7.2"
apps:
  - key: mqtt
    image: eclipse-mosquitto
    tcp:
      - port: 1883
`, nil},
		{"no minCubeship", head + `apps:
  - key: mqtt
    image: eclipse-mosquitto
    tcp:
      - port: 1883
`, []string{"tcp.min-cubeship"}},
		{"a release before TCP ports", head + `minCubeship: ">=0.7.1"
apps:
  - key: mqtt
    image: eclipse-mosquitto
    tcp:
      - port: 1883
`, []string{"tcp.min-cubeship"}},
		{"two copies", head + `minCubeship: "0.7.2"
apps:
  - key: mqtt
    image: eclipse-mosquitto
    scale: 2
    tcp:
      - port: 1883
`, []string{"tcp.one-copy"}},
		{"one container port twice", head + `minCubeship: "0.7.2"
apps:
  - key: mqtt
    image: eclipse-mosquitto
    tcp:
      - port: 1883
      - port: 1883
`, []string{"tcp.duplicate"}},
		{"a host port no instance publishes on", head + `minCubeship: "0.7.2"
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: "22"
`, []string{"tcp.host"}},
		{"a host port answered by text", head + `minCubeship: "0.7.2"
inputs:
  - key: sshPort
    type: text
    label: SSH port
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: ${input.sshPort}
`, []string{"tcp.host"}},
		{"a host port that is not an input", head + `minCubeship: "0.7.2"
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: port-${input.sshPort}
`, []string{"tcp.host"}},
		{"two ports on one host port", head + `minCubeship: "0.7.2"
` + input + `apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: ${input.sshPort}
  - key: other
    image: gitea/gitea
    tcp:
      - port: 22
        host: ${input.sshPort}
`, []string{"tcp.host-duplicate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := codesOf(Validate([]byte(tc.file)).Diagnostics)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A literal host port is decoded whether it is written as a number or
// quoted, and out of range as a number is refused by the shape.
func TestAHostPortIsANumberOrAnInput(t *testing.T) {
	file := `version: 1
project: q
minCubeship: "0.7.2"
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: 2222
      - port: 9418
`
	r := Validate([]byte(file))
	if slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Severity == Error }) {
		t.Fatalf("refused: %+v", r.Diagnostics)
	}
	tcp := r.Manifest.Apps[0].TCP
	if len(tcp) != 2 || tcp[0].Port != 22 || tcp[0].Host == nil || *tcp[0].Host != "2222" || tcp[1].Host != nil {
		t.Fatalf("normalized %+v", tcp)
	}

	low := `version: 1
project: q
minCubeship: "0.7.2"
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: 80
`
	if len(codesOf(Validate([]byte(low)).Diagnostics)) == 0 {
		t.Fatal("host port 80 was accepted")
	}
}

// An input that can be answered with a port no instance publishes on is
// advice, not a refusal.
func TestAnUnboundedHostPortInputIsAdvised(t *testing.T) {
	file := `version: 1
project: q
minCubeship: "0.7.2"
inputs:
  - key: sshPort
    type: number
    label: SSH port
apps:
  - key: git
    image: gitea/gitea
    tcp:
      - port: 22
        host: ${input.sshPort}
`
	r := Validate([]byte(file))
	if !slices.ContainsFunc(r.Diagnostics, func(d Diagnostic) bool { return d.Code == "tcp.host-range" && d.Severity == Warning }) {
		t.Fatalf("no advice: %+v", r.Diagnostics)
	}
	if Blocks(r.Diagnostics) {
		t.Fatalf("advice refused the file: %+v", r.Diagnostics)
	}
}
