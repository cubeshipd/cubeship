// Package firewall is the host's UFW, as this instance sees it: whether
// it is on, what it lets through, and the one thing UFW does not cover
// on a machine running Docker.
//
// **It owns no rows.** The rules live in UFW, which is where the
// operator's own `ufw` command would look for them, and a second copy
// here would be a copy that drifts the first time somebody types
// `ufw allow` over SSH. Same shape as internal/certificates, which reads
// Traefik's store: this module reads and writes the thing that is
// already the truth.
//
// # Docker goes around UFW, and that is the whole reason this is not a
// # thin wrapper
//
// Docker publishes a port by writing DNAT rules of its own. That traffic
// is *forwarded* to a container rather than delivered to the host, so it
// never reaches the INPUT chain UFW governs — and every port Cubeship
// opens is a published container port: Traefik's 80 and 443, an exposed
// datastore's 15000-15999, the daemon itself.
//
// So `ufw deny 15432` would sit in a table looking like protection while
// the port stayed open to the internet. A firewall screen that lies is
// worse than no firewall screen, and it is the ordinary outcome of
// wrapping UFW on a Docker host without knowing this.
//
// What covers those ports is the DOCKER-USER chain, which Docker jumps
// to before its own rules and never rewrites. Sending it through UFW's
// forward chain is what `ufw route allow` then governs — see dockerBlock
// for the stanza that does it and Service.AdoptDocker for when it is
// installed.
package firewall

import (
	"errors"
	"fmt"
	"strings"
)

// Scope is which traffic a rule is about, and it is the distinction the
// whole module turns on.
type Scope string

const (
	// ScopeHost is traffic to the machine itself: SSH, and anything
	// else not in a container. Plain `ufw allow`.
	ScopeHost Scope = "host"

	// ScopeApps is traffic forwarded to a container — every port
	// Cubeship publishes. `ufw route allow`, which only means anything
	// once the DOCKER-USER stanza is installed.
	ScopeApps Scope = "apps"
)

func (s Scope) Valid() bool { return s == ScopeHost || s == ScopeApps }

// Action is what happens to what a rule matches.
type Action string

const (
	ActionAllow  Action = "allow"
	ActionDeny   Action = "deny"
	ActionReject Action = "reject"
)

func (a Action) Valid() bool {
	switch a {
	case ActionAllow, ActionDeny, ActionReject:
		return true
	}
	return false
}

// Protocol is tcp, udp, or empty for both.
type Protocol string

const (
	ProtocolAny Protocol = ""
	ProtocolTCP Protocol = "tcp"
	ProtocolUDP Protocol = "udp"
)

func (p Protocol) Valid() bool {
	switch p {
	case ProtocolAny, ProtocolTCP, ProtocolUDP:
		return true
	}
	return false
}

// Rule is one line of `ufw status numbered`.
//
// It carries the raw text as well as the parsed parts, because UFW's
// output is richer than this struct — an interface, a rate limit, a
// v6 twin — and a rule shown as less than it is would be a rule
// somebody deletes by mistake. Text is what the screen prints; the parts
// are for grouping and for the one thing the daemon has to understand,
// which is whether SSH is still let in.
type Rule struct {
	// Index is the position `ufw status numbered` gave it, which is
	// also how it is deleted. It shifts whenever anything above it
	// goes, so it is read fresh every time and never stored.
	Index int    `json:"index"`
	Text  string `json:"text"`
	Scope Scope  `json:"scope"`

	Action   Action   `json:"action"`
	Protocol Protocol `json:"protocol,omitempty"`
	// Ports is what the rule admits, as UFW spells it: "22", "80,443",
	// "15000:15999". Empty for a rule that names none.
	Ports string `json:"ports,omitempty"`
	// From is where it applies from, empty for anywhere.
	From string `json:"from,omitempty"`
	// Comment is UFW's own, the part after "# ".
	Comment string `json:"comment,omitempty"`
	// V6 marks the IPv6 half of a rule UFW wrote twice. The screen
	// folds these away: two lines for one decision is a list nobody can
	// read.
	V6 bool `json:"v6"`

	// Protected marks a rule this screen will not delete: the one
	// admitting SSH on a running firewall.
	//
	// Not a guess about what somebody wants — it is the same guarantee
	// Enable keeps, from the other side. Enabling writes this rule
	// precisely so the session survives; letting the next click remove
	// it would make that promise last exactly as long as nobody was
	// curious. Deleting it is still possible, on the machine, where the
	// person doing it can see what happens next.
	Protected bool `json:"protected"`

	// Delete is the command that removes this rule, for a rule read
	// while the firewall is off. There are no positions then — `ufw
	// status numbered` answers "inactive" and nothing else — so the
	// only way back out is the rule's own spelling, which is exactly
	// what `ufw show added` prints. Empty for a rule read from the
	// numbered listing, which goes by index.
	Delete []string `json:"-"`
}

// Status is the whole of what this instance can say about the host's
// firewall.
type Status struct {
	// Available is false when the daemon cannot reach the host at all —
	// it is running as a host process rather than a container. Then
	// nothing below is known, and the screen says so rather than
	// showing an empty firewall.
	Available bool `json:"available"`
	// Installed is false when the host has no ufw. Not an error: plenty
	// of machines do not, and saying "not installed" beats a failure
	// that reads like a bug.
	Installed bool `json:"installed"`
	Enabled   bool `json:"enabled"`
	// DefaultIncoming is what happens to traffic no rule matches —
	// "deny" or "allow". It is the single most important fact here and
	// the one people assume rather than check.
	DefaultIncoming string `json:"default_incoming,omitempty"`
	Rules           []Rule `json:"rules"`

	// DockerAdopted says whether the DOCKER-USER stanza is installed —
	// that is, whether an "apps" rule means anything at all. Without
	// it, published container ports are open whatever this list says.
	DockerAdopted bool `json:"docker_adopted"`

	// SSHPorts are the ports the host's sshd said it is listening on.
	// Enabling admits them if nothing else does, which is why they are
	// read from the host rather than assumed.
	//
	// **Empty means the host did not say**, and is not filled in with
	// 22: a guess is harmless while it only decides whether to refuse,
	// and is a lockout once it decides which port to open.
	SSHPorts []int `json:"ssh_ports,omitempty"`
	// SSHAllowed is whether some rule already lets one of those in.
	SSHAllowed bool `json:"ssh_allowed"`

	// YourIP is the address the request asking for this came from, so a
	// screen can offer "just me" without sending somebody to look their
	// own address up. It is what the daemon sees — the forwarded
	// address through Traefik, the socket's otherwise — and it is per
	// request rather than a fact about the host, which is why it is
	// filled in by the handler.
	YourIP string `json:"your_ip,omitempty"`

	// Published are the host ports containers are answering on right
	// now — which, on this machine, is what is actually exposed. They
	// are here because turning on Docker port control starts denying
	// every one of them that no rule admits, and a screen about to do
	// that has to be able to say what it is about to close.
	Published []Published `json:"published"`
}

// Published is one host port a container answers on.
type Published struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Container string `json:"container"`
	// Allowed is whether a rule already admits it, so the screen can
	// offer the ones that would go dark rather than all of them.
	Allowed bool `json:"allowed"`
}

var (
	// ErrNotInstalled is a host with no ufw. Installing one is the
	// operator's call — it is their machine's package manager, and a
	// daemon that installs software on the host uninvited is a
	// different kind of program from this one.
	ErrNotInstalled = errors.New("this host has no ufw installed")

	// ErrWouldLockYouOut refuses enabling a default-deny firewall while
	// nothing admits SSH. Enable now writes that rule itself, so this is
	// only reached through ErrSSHUnknown below.
	ErrWouldLockYouOut = errors.New("no rule admits SSH")

	// ErrSSHUnknown is a host whose sshd did not say which port it is
	// on. It is the one case where enabling cannot be made safe: any
	// rule written here would be a guess, and a wrong guess is exactly
	// the lockout the check exists to prevent.
	ErrSSHUnknown = fmt.Errorf(
		"%w, and this daemon could not find out which port sshd is on. Add a rule for the port you connect on, then turn it on",
		ErrWouldLockYouOut)

	// ErrBadRule is a rule that does not describe anything.
	ErrBadRule = errors.New("invalid rule")

	// ErrNoSuchRule is deleting a position that is not there — usually
	// a screen acting on a listing something else has changed since.
	ErrNoSuchRule = errors.New("no such rule")

	// ErrKeepsYouIn refuses deleting the rule that admits SSH while the
	// firewall is running.
	ErrKeepsYouIn = errors.New("that rule is what admits SSH")

	// ErrRuleChanged is deleting a position that holds something other
	// than what the caller was looking at.
	ErrRuleChanged = errors.New("that rule is not the one you were looking at; the list changed")

	// ErrDockerNotAdopted refuses an "apps" rule while the DOCKER-USER
	// stanza is missing. Writing one anyway would put a line in the
	// table that governs nothing, which is the exact lie this module
	// exists to avoid.
	ErrDockerNotAdopted = errors.New("published container ports are not governed by ufw on this host yet")
)

// KeepsYouInError says why, and where it can be done instead.
//
// It names the command rather than saying "do it on the machine",
// because somebody who has decided they mean it should not also have to
// go and look up the syntax.
func KeepsYouInError(rule Rule) error {
	return fmt.Errorf(
		"%w, and the firewall is on: deleting it here would end this session and every other one. If you mean it, do it on the machine, where you can see what happens next — %s",
		ErrKeepsYouIn, rule.DeleteCommand())
}

// DeleteCommand is how this rule is removed by hand.
//
// Not Text with "delete" in front of it: what `ufw status` prints is a
// padded table — "22/tcp   ALLOW IN   Anywhere" — and handing that back
// as a command would be handing somebody a line that does not run. UFW
// takes a rule back in the form it was given in, so it is rebuilt from
// the parts.
func (r Rule) DeleteCommand() string {
	if len(r.Delete) > 0 {
		// Read from `ufw show added`, which prints commands already.
		return strings.Join(r.Delete, " ")
	}
	if r.Action == "" || r.Ports == "" {
		// Nothing reliable to rebuild from, so the position it is at —
		// which is only meaningful next to the listing it came from.
		return fmt.Sprintf("ufw status numbered, then ufw delete %d", r.Index)
	}

	out := "ufw "
	if r.Scope == ScopeApps {
		out += "route "
	}
	out += "delete " + string(r.Action) + " "
	if r.From != "" {
		out += "from " + r.From + " to any port " + r.Ports
		if r.Protocol != ProtocolAny {
			out += " proto " + string(r.Protocol)
		}
		return out
	}
	out += r.Ports
	if r.Protocol != ProtocolAny {
		out += "/" + string(r.Protocol)
	}
	return out
}

// LockedOutError names the ports that would have had to be allowed.
func LockedOutError(ports []int) error {
	return fmt.Errorf("%w: enabling would end this SSH session and every other one. Allow %s first",
		ErrWouldLockYouOut, portList(ports))
}

func portList(ports []int) string {
	if len(ports) == 0 {
		return "the port you reach this host on"
	}
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("port %d", p))
	}
	return strings.Join(out, " or ")
}
