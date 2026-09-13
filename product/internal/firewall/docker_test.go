package firewall

import (
	"errors"
	"strings"
	"testing"
)

// With nothing exposed the stanza is the one that shipped: no conntrack
// line, and no comment introducing one.
func TestTheStanzaWithNothingExposed(t *testing.T) {
	block, err := renderDockerBlock(nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(block, "conntrack") {
		t.Errorf("a stanza with nothing exposed has a conntrack line:\n%s", block)
	}
	if !strings.HasPrefix(block, dockerBeginMarker+"\n") || !strings.HasSuffix(block, dockerEndMarker+"\n") {
		t.Errorf("the stanza does not run marker to marker:\n%s", block)
	}
}

// One line per port, sorted and once each, after every RETURN that lets
// traffic through and before the first denial — anywhere later and the
// denial has already dropped the SYN.
func TestTheStanzaReturnsEachExposedPortOnce(t *testing.T) {
	block, err := renderDockerBlock([]int{15002, 16000, 15000, 15002})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	lines := strings.Split(block, "\n")

	var exposed []string
	firstExposed, lastExposed, dns, firstDeny := -1, -1, -1, -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "--ctorigdstport"):
			exposed = append(exposed, line)
			if firstExposed == -1 {
				firstExposed = i
			}
			lastExposed = i
		case strings.Contains(line, "--sport 53"):
			dns = i
		case strings.HasPrefix(line, "-A DOCKER-USER -j ufw-docker-logging-deny") && firstDeny == -1:
			firstDeny = i
		}
	}

	want := []string{
		"-A DOCKER-USER -p tcp -m conntrack --ctorigdstport 15000 --ctdir ORIGINAL -j RETURN",
		"-A DOCKER-USER -p tcp -m conntrack --ctorigdstport 15002 --ctdir ORIGINAL -j RETURN",
		"-A DOCKER-USER -p tcp -m conntrack --ctorigdstport 16000 --ctdir ORIGINAL -j RETURN",
	}
	if strings.Join(exposed, "\n") != strings.Join(want, "\n") {
		t.Errorf("exposed lines:\n%s\nwant:\n%s", strings.Join(exposed, "\n"), strings.Join(want, "\n"))
	}
	if dns == -1 || firstDeny == -1 || firstExposed < dns || lastExposed > firstDeny {
		t.Errorf("exposed lines at %d-%d, DNS RETURN at %d, first deny at %d:\n%s",
			firstExposed, lastExposed, dns, firstDeny, block)
	}
	if forward := strings.Index(block, "-j ufw-user-forward"); forward == -1 || forward > strings.Index(block, want[0]) {
		t.Errorf("the exposed lines come before ufw-user-forward:\n%s", block)
	}
}

// Nothing but a port reaches a root-owned file.
func TestTheStanzaRefusesWhatIsNotAPort(t *testing.T) {
	for _, bad := range []int{0, 65536, -1} {
		if _, err := renderDockerBlock([]int{15000, bad}); !errors.Is(err, ErrBadRule) {
			t.Errorf("port %d: %v", bad, err)
		}
	}
	if _, err := renderDockerBlock([]int{1, 65535}); err != nil {
		t.Errorf("the ends of the range: %v", err)
	}
}

// What Status reads back is exactly what the renderer writes, and a line
// that merely looks similar is not taken for one.
func TestTheExposedLinesReadBack(t *testing.T) {
	block, err := renderDockerBlock([]int{15002, 15000})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	got := parseExposedRules(block + "\n-A DOCKER-USER -p udp -m conntrack --ctorigdstport 9 --ctdir ORIGINAL -j RETURN\n")
	if len(got) != 2 || !got[15000] || !got[15002] {
		t.Errorf("read back %v", got)
	}
}
