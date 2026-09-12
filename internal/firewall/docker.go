package firewall

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The stanza that makes a published container port answerable to UFW.
//
// # Why anything is needed at all
//
// Docker publishes a port by writing its own DNAT and filter rules
// straight into netfilter, ahead of UFW's. The traffic is *forwarded* to
// a container rather than delivered to the host, so it never passes
// through the INPUT chain UFW governs — `ufw deny 15432` is a line in a
// table that this traffic does not visit.
//
// Docker leaves exactly one seam: it jumps to DOCKER-USER before its own
// rules, and it never rewrites that chain. Everything below is a way of
// sending that jump through UFW first.
//
// # What it does, line by line
//
//   - `-j ufw-user-forward` first, so anything an operator allowed with
//     `ufw route allow` is accepted before any of the denials below are
//     reached. That is the whole point: it is what turns a rule on the
//     screen into a rule that governs a container.
//   - RETURN for traffic *from* the private ranges, so containers can
//     talk to each other and to the host's own network as they always
//     did. Without this, adopting would cut every app off from its
//     database.
//   - RETURN for DNS answers, which arrive as UDP from port 53 to a high
//     port and would otherwise be caught by the blanket UDP denial.
//   - RETURN for every port this instance published on purpose — an
//     exposed datastore or managed object store — matched on the number
//     it was published on. See renderDockerBlock.
//   - Deny new connections *to* the private ranges — which is where
//     containers live, so this is the denial that does the work. It is
//     scoped to SYN for TCP so that established traffic is untouched.
//   - RETURN at the end, so nothing this stanza did not mean to catch is
//     affected.
//
// The shape is the well-trodden one (`ufw-docker` and the Docker
// documentation's own note about UFW), which matters: this is the kind
// of file where being clever is how a machine ends up unreachable.
//
// # Why it is appended to after.rules
//
// UFW loads `/etc/ufw/after.rules` after its own, on every reload and
// every boot, and it is a file it will not rewrite. Anything else — a
// systemd unit, an iptables command at start-up — has to be re-applied
// by something, and the something is exactly what will not be there
// after a reboot at 4am.
const dockerBlockHead = dockerBeginMarker + `
# Written by Cubeship. Everything between these two markers is managed;
# remove it from the Firewall screen rather than by hand, so the rules
# and the state agree.
#
# It exists because Docker publishes ports by writing netfilter rules
# ahead of ufw's own, so a published container port is not governed by
# "ufw deny" at all. DOCKER-USER is the one chain Docker jumps to first
# and never rewrites; sending it through ufw-user-forward is what makes
# "ufw route allow <port>" mean something.
*filter
:ufw-user-forward - [0:0]
:ufw-docker-logging-deny - [0:0]
:DOCKER-USER - [0:0]

# Anything explicitly allowed with "ufw route allow" is accepted here,
# before any denial below is reached.
-A DOCKER-USER -j ufw-user-forward

# Traffic from the private ranges is left alone: containers reaching
# each other, and the host reaching them.
-A DOCKER-USER -j RETURN -s 10.0.0.0/8
-A DOCKER-USER -j RETURN -s 172.16.0.0/12
-A DOCKER-USER -j RETURN -s 192.168.0.0/16

# DNS answers, which come back as UDP from port 53 to a high port.
-A DOCKER-USER -p udp -m udp --sport 53 --dport 1024:65535 -j RETURN
`

// dockerBlockExposed introduces the lines renderDockerBlock adds, and is
// left out entirely when there are none.
const dockerBlockExposed = `
# Ports this instance published on purpose: an exposed database or object
# store. Matched on the port as it was published, which conntrack keeps
# after Docker's DNAT has rewritten it; a ufw rule only ever sees the port
# inside the container, which every database of one engine shares.
# TCP and IPv4 only, like everything they serve and like this file.
`

const dockerBlockTail = `
# New connections into the ranges containers live in are denied. This is
# the line that closes a published port nobody allowed.
-A DOCKER-USER -j ufw-docker-logging-deny -p tcp -m tcp --tcp-flags FIN,SYN,RST,ACK SYN -d 192.168.0.0/16
-A DOCKER-USER -j ufw-docker-logging-deny -p tcp -m tcp --tcp-flags FIN,SYN,RST,ACK SYN -d 10.0.0.0/8
-A DOCKER-USER -j ufw-docker-logging-deny -p tcp -m tcp --tcp-flags FIN,SYN,RST,ACK SYN -d 172.16.0.0/12
-A DOCKER-USER -j ufw-docker-logging-deny -p udp -m udp --dport 0:32767 -d 192.168.0.0/16
-A DOCKER-USER -j ufw-docker-logging-deny -p udp -m udp --dport 0:32767 -d 10.0.0.0/8
-A DOCKER-USER -j ufw-docker-logging-deny -p udp -m udp --dport 0:32767 -d 172.16.0.0/12

-A DOCKER-USER -j RETURN

-A ufw-docker-logging-deny -m limit --limit 3/min --limit-burst 10 -j LOG --log-prefix "[UFW DOCKER BLOCK] "
-A ufw-docker-logging-deny -j DROP

COMMIT
` + dockerEndMarker + `
`

// exposedRulePrefix and exposedRuleSuffix surround the port in each line
// renderDockerBlock writes, and are what Status reads those lines back by.
const (
	exposedRulePrefix = "-A DOCKER-USER -p tcp -m conntrack --ctorigdstport "
	exposedRuleSuffix = " --ctdir ORIGINAL -j RETURN"
)

// renderDockerBlock is the stanza, with a RETURN for each published port
// in exposed.
//
// It starts at the begin marker and ends with a newline after the end
// marker; whoever writes it decides what goes before.
//
// The lines sit after the private-range and DNS RETURNs and before the
// first denial. RETURN rather than ACCEPT hands the packet back to
// Docker's own rules, as the RETURNs above it do, and a chain of their
// own is not an option: a RETURN from a sub-chain lands back here and
// falls into the denials.
//
// Not a container's address, which changes on every restart and would
// open whatever container is handed it next.
func renderDockerBlock(exposed []int) (string, error) {
	ports := slices.Clone(exposed)
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return "", fmt.Errorf("%w: %d is not a port", ErrBadRule, p)
		}
	}
	slices.Sort(ports)
	ports = slices.Compact(ports)

	var b strings.Builder
	b.WriteString(dockerBlockHead)
	if len(ports) > 0 {
		b.WriteString(dockerBlockExposed)
		for _, p := range ports {
			b.WriteString(exposedRulePrefix + strconv.Itoa(p) + exposedRuleSuffix + "\n")
		}
	}
	b.WriteString(dockerBlockTail)
	return b.String(), nil
}

// parseExposedRules reads the ports back out of the lines renderDockerBlock
// wrote, ignoring anything else.
func parseExposedRules(out string) map[int]bool {
	ports := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), exposedRulePrefix)
		if !ok {
			continue
		}
		number, ok := strings.CutSuffix(rest, exposedRuleSuffix)
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(number); err == nil && n > 0 && n < 65536 {
			ports[n] = true
		}
	}
	return ports
}

// The markers are what makes this removable without touching a line
// somebody else wrote in the same file.
const (
	dockerBeginMarker = "# BEGIN CUBESHIP DOCKER"
	dockerEndMarker   = "# END CUBESHIP DOCKER"
)
