package dns_test

import (
	"testing"

	"cubeship/internal/dns"
)

// A CNAME cannot share a name with anything else. Both providers refuse
// the write rather than making room, and the message they return names
// the record already there — so the caller is told about something they
// did not ask for, on a screen that had just offered to overwrite.
//
// This is the rule that pass has to obey. It is a table because the
// asymmetry is the part that was wrong: only the same-type case was
// being cleared, so an A over a CNAME failed every time.
func TestWhatStandsInTheWayOfARecord(t *testing.T) {
	tests := []struct {
		existing string
		wanted   string
		want     bool
	}{
		// Replacing a record means clearing what is there, not adding a
		// second answer beside it.
		{"A", "A", true},
		{"TXT", "TXT", true},

		// A CNAME excludes everything, in both directions.
		{"CNAME", "A", true},
		{"A", "CNAME", true},
		{"CNAME", "TXT", true},
		{"TXT", "CNAME", true},
		{"CNAME", "CNAME", true},

		// Everything else coexists: a name can hold an A and a TXT at
		// once, and clearing one to write the other would delete a
		// record nobody mentioned.
		{"A", "TXT", false},
		{"TXT", "A", false},
		{"A", "AAAA", false},
		{"MX", "TXT", false},
	}

	for _, tt := range tests {
		if got := dns.Conflicts(tt.existing, tt.wanted); got != tt.want {
			t.Errorf("writing %s where a %s is: got %v, want %v",
				tt.wanted, tt.existing, got, tt.want)
		}
	}
}
