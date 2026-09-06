package firewall

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
const dockerBlock = `
` + dockerBeginMarker + `
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

// The markers are what makes this removable without touching a line
// somebody else wrote in the same file.
const (
	dockerBeginMarker = "# BEGIN CUBESHIP DOCKER"
	dockerEndMarker   = "# END CUBESHIP DOCKER"
)
