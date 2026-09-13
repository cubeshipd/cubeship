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

// A container reaching this machine's own address lands in INPUT, so the
// published ports are accepted there from Docker's bridges — Traefik's
// always, an exposed one when there is one — and nothing else is.
func TestTheStanzaLetsContainersReachWhatIsPublished(t *testing.T) {
	block, err := renderDockerBlock([]int{15432})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var got []string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "-A ufw-after-input") {
			got = append(got, line)
		}
	}
	want := []string{
		"-A ufw-after-input -i br-+ -p tcp -m multiport --dports 80,443,15432 -j ACCEPT",
		"-A ufw-after-input -i docker0 -p tcp -m multiport --dports 80,443,15432 -j ACCEPT",
		"-A ufw-after-input -i docker_gwbridge -p tcp -m multiport --dports 80,443,15432 -j ACCEPT",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("INPUT lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if strings.Contains(block, ":ufw-after-input") {
		t.Errorf("the stanza declares ufw-after-input, which flushes ufw's own lines in it:\n%s", block)
	}
	if strings.Index(block, want[0]) > strings.Index(block, "COMMIT") {
		t.Errorf("the INPUT lines come after COMMIT:\n%s", block)
	}
}

// multiport names at most fifteen ports, so more are split across rules.
func TestTheInputLinesSplitWhatMultiportCannotCarry(t *testing.T) {
	var exposed []int
	for p := 15000; p < 15020; p++ {
		exposed = append(exposed, p)
	}
	rules := hairpinRules(exposed)
	if len(rules) != 2*len(hairpinInterfaces) {
		t.Fatalf("got %d rules:\n%s", len(rules), strings.Join(rules, "\n"))
	}
	for _, r := range rules {
		dports := strings.Fields(strings.SplitN(r, "--dports ", 2)[1])[0]
		if n := len(strings.Split(dports, ",")); n > multiportMax {
			t.Errorf("%d ports in one rule: %s", n, r)
		}
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
