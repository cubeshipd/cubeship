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
