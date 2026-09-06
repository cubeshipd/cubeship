package certificates

import (
	"strings"
	"testing"
)

// Traefik's log is the only place an ACME failure is written down, so
// what this pulls out of it is the difference between "pending" and
// knowing why.
func TestWhatIsTakenOutOfTraefiksLog(t *testing.T) {
	// A frame header's control bytes in front of a line, the way Docker
	// hands them over, and the same refusal repeated the way a retry
	// loop produces it.
	log := strings.Join([]string{
		"\x01\x00\x00\x00\x00\x00\x00\x4dtime=\"2026-09-06T11:00:00Z\" level=info msg=\"Configuration loaded\"",
		"time=\"2026-09-06T12:00:00Z\" level=error msg=\"Unable to obtain ACME certificate for domains \\\"registry.75-119-141-235.sslip.io\\\": error: one or more domains had a problem: acme: error: 429 :: too many certificates already issued for sslip.io\"",
		"time=\"2026-09-06T12:30:00Z\" level=error msg=\"Unable to obtain ACME certificate for domains \\\"registry.75-119-141-235.sslip.io\\\": error: one or more domains had a problem: acme: error: 429 :: too many certificates already issued for sslip.io\"",
		"time=\"2026-09-06T13:00:00Z\" level=error msg=\"Unable to obtain ACME certificate for domains \\\"api.example.com\\\": timeout\"",
		"time=\"2026-09-06T13:05:00Z\" level=debug msg=\"Serving default certificate\"",
	}, "\n")

	lines := complaints(strings.NewReader(log))
	if len(lines) != 2 {
		t.Fatalf("kept %d lines, want the two distinct failures: %v", len(lines), lines)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "\x00\x01") {
			t.Errorf("a frame header's bytes came through: %q", line)
		}
	}

	// Each name gets the last thing said about it, and a name nothing
	// was said about gets nothing.
	if got := about(lines, "registry.75-119-141-235.sslip.io"); !strings.Contains(got, "too many certificates") {
		t.Errorf("the registry's line is %q", got)
	}
	if got := about(lines, "api.example.com"); !strings.Contains(got, "timeout") {
		t.Errorf("the api's line is %q", got)
	}
	if got := about(lines, "quiet.example.com"); got != "" {
		t.Errorf("a name nothing was said about got %q", got)
	}
}

// The lines that name no host are the ones worth keeping too: on a
// default install the commonest failure is a rate limit against the
// whole of sslip.io, and that refusal is about the service rather than
// about any one name.
func TestAComplaintThatNamesNoHostIsStillKept(t *testing.T) {
	log := `time="2026-09-06T12:00:00Z" level=error msg="Unable to obtain ACME certificate: acme: error: 429 :: too many certificates already issued"`

	lines := complaints(strings.NewReader(log))
	if len(lines) != 1 {
		t.Fatalf("kept %v", lines)
	}
	if about(lines, "registry.example.com") != "" {
		t.Error("a line naming no host was attributed to one")
	}
}

// The failure that hides behind "waiting".
//
// Traefik's Docker provider asks the Engine for a fixed API version.
// An Engine that refuses it leaves Traefik seeing no container at all,
// so every router that comes from a label does not exist and every name
// behind one is served the default self-signed certificate — while the
// file provider carries on, which is why the instance's own name still
// has a certificate and the registry's does not.
//
// Nothing in the ACME wording says any of that, because ACME is never
// reached. The provider's own line is the only place it is written
// down, so it has to survive the filter.
func TestAProviderThatCannotReadTheEngineIsAComplaint(t *testing.T) {
	// The retry loop, with its own backoff on every line — which is
	// what made the timestamp alone useless as a key.
	log := strings.Join([]string{
		`time="2026-09-06T07:09:56Z" level=error msg="Failed to retrieve information of the docker client and server host" error="Error response from daemon: client version 1.24 is too old. Minimum supported API version is 1.40, please upgrade your client to a newer version" providerName=docker`,
		`time="2026-09-06T07:09:56Z" level=error msg="Provider error, retrying in 6.055130443s" error="Error response from daemon: client version 1.24 is too old. Minimum supported API version is 1.40" providerName=docker`,
		`time="2026-09-06T07:10:02Z" level=error msg="Provider error, retrying in 4.305096518s" error="Error response from daemon: client version 1.24 is too old. Minimum supported API version is 1.40" providerName=docker`,
		`time="2026-09-06T07:10:06Z" level=info msg="Starting provider *docker.Provider"`,
	}, "\n")

	lines := complaints(strings.NewReader(log))
	if len(lines) != 2 {
		t.Fatalf("kept %d lines, want the two distinct ones: %v", len(lines), lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "1.24 is too old") {
			t.Errorf("a line came through without the reason: %q", line)
		}
	}
}
