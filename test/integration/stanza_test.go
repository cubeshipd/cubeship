//go:build integration

// What Cubeship writes into the host's after.rules, read by iptables.
//
// The unit tests render the stanza and look at the string, which proves
// the lines are where they were meant to be and nothing about whether
// iptables will load them — the same gap TestTraefikAcceptsABalancedRoute
// exists for. Here it costs more than a proxy: ufw loads this file on
// every reload and every boot, and a file it cannot load is a firewall
// that does not come up.
//
// So this hands the rendered stanza to a real iptables-restore, in a
// throwaway container with NET_ADMIN and a network namespace of its own,
// with ufw's chains declared first the way ufw.rules would have. --test
// parses and checks the whole batch against the kernel and commits
// nothing; the namespace is the container's, so even a commit would
// touch nothing on the machine running the test.

package integration

import (
	"os/exec"
	"strings"
	"testing"

	"cubeship/internal/firewall"
)

// iptablesImage is where iptables-restore comes from: the daemon's own
// base, with the package added at run time.
const iptablesImage = "alpine:3.21"

// ufwChains is what ufw has already created by the time it loads
// after.rules, and what the stanza jumps to.
const ufwChains = `*filter
:ufw-user-forward - [0:0]
COMMIT
`

// iptablesRefuses loads ufwChains, then asks iptables-restore whether it
// would accept rules on top of them, the way ufw applies after.rules:
// without flushing what is already there. It returns what iptables said
// when it refused, and "" when it accepted.
func iptablesRefuses(t *testing.T, rules string) string {
	t.Helper()
	// The nf_tables backend when the image has one, which is what the
	// target hosts run.
	script := `
set -e
apk add --no-cache iptables >/dev/null
restore=iptables-restore
if command -v iptables-nft-restore >/dev/null 2>&1; then restore=iptables-nft-restore; fi
$restore --version
printf '%s' "$UFW_CHAINS" | $restore
$restore --noflush --test
`
	cmd := exec.Command("docker", "run", "--rm", "-i",
		"--cap-add", "NET_ADMIN",
		"-e", "UFW_CHAINS="+ufwChains,
		iptablesImage, "sh", "-c", script)
	cmd.Stdin = strings.NewReader(rules)
	out, err := cmd.CombinedOutput()
	t.Logf("iptables-restore:\n%s", out)
	if err != nil {
		if _, exited := err.(*exec.ExitError); !exited {
			t.Fatalf("run iptables-restore: %v", err)
		}
		return string(out)
	}
	return ""
}

// The stanza adopting writes on an instance with nothing exposed.
func TestIptablesAcceptsTheStanzaWithNothingExposed(t *testing.T) {
	stanza, err := firewall.DockerStanza(nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if said := iptablesRefuses(t, stanza); said != "" {
		t.Errorf("iptables refused the stanza with nothing exposed:\n%s", said)
	}
}

// And with the conntrack lines: the original destination port, matched
// after Docker's DNAT has rewritten the packet's own.
func TestIptablesAcceptsTheStanzaWithExposedPorts(t *testing.T) {
	stanza, err := firewall.DockerStanza([]int{15000, 15002, 16000})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(stanza, "--ctorigdstport 15002") {
		t.Fatalf("the stanza has no conntrack line to test:\n%s", stanza)
	}
	if said := iptablesRefuses(t, stanza); said != "" {
		t.Errorf("iptables refused the stanza with exposed ports:\n%s", said)
	}
}

// The control. Without it, a harness that accepted everything — a
// --test that checked nothing, an iptables that was never installed —
// would pass both tests above.
func TestIptablesRefusesAStanzaItCannotLoad(t *testing.T) {
	stanza, err := firewall.DockerStanza([]int{15002})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	broken := strings.Replace(stanza, "COMMIT",
		"-A DOCKER-USER -m cubeship-no-such-match -j RETURN\nCOMMIT", 1)
	if said := iptablesRefuses(t, broken); said == "" {
		t.Error("iptables accepted a match that does not exist, so the tests above prove nothing")
	}
}
