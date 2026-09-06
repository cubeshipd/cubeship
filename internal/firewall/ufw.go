package firewall

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// This file is everything that knows how `ufw` spells things: how to
// read what it prints, and how to build what it is told.
//
// It is parsing a human-readable listing, which is not a contract — UFW
// has no machine-readable output, and this is the same bargain
// internal/certificates makes with Traefik's log. What follows from that
// is the rule that a line which does not parse is *kept*, not dropped:
// Rule.Text is what the screen prints, and the parsed fields are for the
// two things the daemon has to understand — whether SSH is still let in,
// and whether a rule is about the host or about a container.

// numbered matches one line of `ufw status numbered`:
//
//	[ 1] 22/tcp                     ALLOW IN    Anywhere
var numbered = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.*)$`)

// columns are the runs of two or more spaces UFW pads its table with. A
// single space is inside a field — "ALLOW IN" is one column.
var columns = regexp.MustCompile(`\s{2,}`)

// defaults matches the policy line of `ufw status verbose`:
//
//	Default: deny (incoming), allow (outgoing), disabled (routed)
var defaults = regexp.MustCompile(`(?i)Default:\s*(\w+)\s*\(incoming\)`)

// parseStatus reads what `ufw status verbose` and `ufw status numbered`
// printed, in that order, separated by statusSeparator.
func parseStatus(out string) (enabled bool, defaultIncoming string, rules []Rule) {
	verbose, numberedOut, found := strings.Cut(out, statusSeparator)
	if !found {
		// One command answered and the other did not. Read what there
		// is rather than nothing: a status with no rules is still worth
		// more than an empty screen.
		verbose, numberedOut = out, out
	}

	enabled = strings.Contains(strings.ToLower(verbose), "status: active")
	if m := defaults.FindStringSubmatch(verbose); m != nil {
		defaultIncoming = strings.ToLower(m[1])
	}
	return enabled, defaultIncoming, parseRules(numberedOut)
}

// statusSeparator is what the two listings are split on. Printed by the
// script that runs them, so it never appears in UFW's own output.
const statusSeparator = "--- cubeship ---"

func parseRules(out string) []Rule {
	var rules []Rule
	for _, line := range strings.Split(out, "\n") {
		m := numbered.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		index, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		rules = append(rules, parseRule(index, strings.TrimSpace(m[2])))
	}
	return rules
}

func parseRule(index int, body string) Rule {
	r := Rule{Index: index, Text: body, Scope: ScopeHost}

	// The comment comes last and may contain anything, including the
	// column padding — so it is taken off before the columns are split.
	if to, comment, found := strings.Cut(body, "# "); found {
		r.Comment = strings.TrimSpace(comment)
		body = strings.TrimSpace(to)
	}

	fields := columns.Split(body, -1)
	if len(fields) < 2 {
		return r
	}
	to, action := fields[0], fields[1]
	if len(fields) > 2 {
		r.From = strings.TrimSpace(fields[2])
	}

	// "(v6)" is UFW showing the second half of a rule it wrote twice.
	// It is marked rather than dropped here — dropping is the screen's
	// decision, and a report that silently halves the list would be
	// wrong for anyone reading it through the API.
	if strings.Contains(to, "(v6)") || strings.Contains(r.From, "(v6)") {
		r.V6 = true
		to = strings.TrimSpace(strings.ReplaceAll(to, "(v6)", ""))
		r.From = strings.TrimSpace(strings.ReplaceAll(r.From, "(v6)", ""))
	}
	if strings.EqualFold(r.From, "Anywhere") {
		r.From = ""
	}

	// The action column is a verb and a direction: "ALLOW IN",
	// "DENY FWD". FWD is the one that matters — it is a routed rule,
	// which on this host means a container's published port.
	verb, direction, _ := strings.Cut(action, " ")
	switch strings.ToUpper(strings.TrimSpace(verb)) {
	case "ALLOW":
		r.Action = ActionAllow
	case "DENY":
		r.Action = ActionDeny
	case "REJECT":
		r.Action = ActionReject
	}
	if strings.EqualFold(strings.TrimSpace(direction), "FWD") {
		r.Scope = ScopeApps
	}

	// The destination is "<ports>/<proto>", "<ports>", or a name UFW
	// knows from /etc/services. Only the first is something the daemon
	// has to understand.
	if ports, proto, found := strings.Cut(to, "/"); found {
		r.Ports = strings.TrimSpace(ports)
		switch strings.ToLower(strings.TrimSpace(proto)) {
		case "tcp":
			r.Protocol = ProtocolTCP
		case "udp":
			r.Protocol = ProtocolUDP
		}
	} else {
		r.Ports = strings.TrimSpace(to)
	}
	return r
}

// Spec is a rule as somebody asks for it.
type Spec struct {
	Scope    Scope
	Action   Action
	Protocol Protocol
	// Port is a single port, or a range as UFW spells one: "15000:15999".
	Port string
	// From is a source address or CIDR. Empty means anywhere, which is
	// what most rules mean.
	From    string
	Comment string
}

// port is what may appear in a port field: a number, or a range.
var port = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?$`)

// source is a source address: an IPv4 or IPv6 literal, optionally with a
// prefix length. Deliberately strict — this string reaches a command
// line, and the guarantee that it is only ever digits, dots, colons and
// a slash is worth more than accepting a hostname UFW would resolve.
var source = regexp.MustCompile(`^[0-9a-fA-F:.]{2,45}(/\d{1,3})?$`)

// comment is what UFW will carry in a `comment` argument without the
// shell or its own parser having an opinion.
var comment = regexp.MustCompile(`^[A-Za-z0-9 ._:@\-]{0,64}$`)

// Check validates a spec, and is the only thing standing between a
// request body and a command run as root on the host.
//
// Every field is matched against a pattern rather than escaped. Escaping
// is a judgement about a shell; this is a statement about what the value
// may contain at all, and it is the kind that does not rot when the way
// the command is run changes.
func (s Spec) Check() error {
	if !s.Scope.Valid() {
		return fmt.Errorf(`%w: scope must be "host" or "apps"`, ErrBadRule)
	}
	if !s.Action.Valid() {
		return fmt.Errorf(`%w: action must be "allow", "deny" or "reject"`, ErrBadRule)
	}
	if !s.Protocol.Valid() {
		return fmt.Errorf(`%w: protocol must be "tcp", "udp" or empty for both`, ErrBadRule)
	}
	if !port.MatchString(s.Port) {
		return fmt.Errorf(`%w: port must be a number, or a range like "15000:15999"`, ErrBadRule)
	}
	if s.From != "" && !source.MatchString(s.From) {
		return fmt.Errorf("%w: from must be an address or a CIDR", ErrBadRule)
	}
	if !comment.MatchString(s.Comment) {
		return fmt.Errorf("%w: a comment is letters, digits and punctuation, up to 64 characters", ErrBadRule)
	}
	// A port range needs a protocol. UFW refuses one without, because
	// it cannot write a range into both tables at once — and its
	// refusal arrives as a usage message nobody reads as being about
	// this.
	if strings.Contains(s.Port, ":") && s.Protocol == ProtocolAny {
		return fmt.Errorf("%w: a port range has to say tcp or udp", ErrBadRule)
	}
	return nil
}

// Args builds the ufw command line for this spec.
//
// Written out as argv rather than a string: nothing here is ever handed
// to a shell, so a value that somehow got past Check still cannot become
// a second command.
func (s Spec) Args() []string {
	args := []string{"ufw"}
	if s.Scope == ScopeApps {
		// `route` is what governs forwarded traffic — a container's
		// published port — rather than traffic to the host itself.
		args = append(args, "route")
	}
	args = append(args, string(s.Action))

	if s.Protocol != ProtocolAny {
		args = append(args, "proto", string(s.Protocol))
	}
	if s.From != "" {
		args = append(args, "from", s.From)
	} else {
		args = append(args, "from", "any")
	}
	args = append(args, "to", "any", "port", s.Port)

	if s.Comment != "" {
		args = append(args, "comment", s.Comment)
	}
	return args
}
