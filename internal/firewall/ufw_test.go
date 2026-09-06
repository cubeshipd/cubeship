package firewall

import (
	"strings"
	"testing"
)

// What UFW prints, as it prints it: a padded table, an action column
// with a space in it, a comment on the end, and the same rule again for
// IPv6.
const sample = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW IN    Anywhere
80,443/tcp                 ALLOW IN    Anywhere
` + statusSeparator + `
Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80,443/tcp                 ALLOW FWD   Anywhere                   # cubeship
[ 3] 15432/tcp                  ALLOW FWD   10.0.0.0/8
[ 4] 5432                       DENY IN     Anywhere
[ 5] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
`

func TestReadingWhatUFWPrints(t *testing.T) {
	enabled, defaultIncoming, rules := parseStatus(sample)

	if !enabled {
		t.Error("an active firewall was read as off")
	}
	if defaultIncoming != "deny" {
		t.Errorf("default incoming is %q", defaultIncoming)
	}
	if len(rules) != 5 {
		t.Fatalf("read %d rules: %+v", len(rules), rules)
	}

	// A rule to the host, and one to a container. The difference is the
	// whole module: "IN" is the machine, "FWD" is a published port.
	if rules[0].Scope != ScopeHost || rules[0].Action != ActionAllow ||
		rules[0].Ports != "22" || rules[0].Protocol != ProtocolTCP {
		t.Errorf("the SSH rule read as %+v", rules[0])
	}
	if rules[1].Scope != ScopeApps || rules[1].Ports != "80,443" {
		t.Errorf("a routed rule read as %+v", rules[1])
	}
	if rules[1].Comment != "cubeship" {
		t.Errorf("the comment read as %q", rules[1].Comment)
	}
	if rules[2].From != "10.0.0.0/8" {
		t.Errorf("the source read as %q", rules[2].From)
	}
	if rules[3].Action != ActionDeny || rules[3].Protocol != ProtocolAny {
		t.Errorf("a deny with no protocol read as %+v", rules[3])
	}

	// The v6 twin is marked, not dropped. One decision, two lines: a
	// screen folds them, and an API client is told which is which.
	if !rules[4].V6 {
		t.Errorf("the v6 half was not marked: %+v", rules[4])
	}
	if rules[4].Ports != "22" || rules[4].From != "" {
		t.Errorf("the v6 half kept its marker: %+v", rules[4])
	}

	// Whatever else UFW said about a rule is still there to print.
	if !strings.Contains(rules[0].Text, "ALLOW IN") {
		t.Errorf("the raw line was lost: %q", rules[0].Text)
	}
}

// A line this daemon cannot make sense of is kept, not hidden. UFW's
// syntax is wider than these fields — an interface, a rate limit — and a
// firewall screen that silently omits a rule is a screen that gets
// somebody to open a port that was already open.
func TestARuleThisDoesNotUnderstandIsStillListed(t *testing.T) {
	rules := parseRules("[ 1] Anywhere on eth0           ALLOW IN    Anywhere\n")
	if len(rules) != 1 {
		t.Fatalf("kept %d", len(rules))
	}
	if !strings.Contains(rules[0].Text, "eth0") {
		t.Errorf("the line did not survive: %q", rules[0].Text)
	}
}

// Enabling is refused unless SSH is admitted, so the check has to
// recognise the ways somebody may have admitted it.
func TestWhichRulesCountAsAdmittingSSH(t *testing.T) {
	for _, tt := range []struct {
		name  string
		rules []Rule
		want  bool
	}{
		{"the plain one", []Rule{{Action: ActionAllow, Scope: ScopeHost, Ports: "22"}}, true},
		{"in a list", []Rule{{Action: ActionAllow, Scope: ScopeHost, Ports: "22,80,443"}}, true},
		{"in a range", []Rule{{Action: ActionAllow, Scope: ScopeHost, Ports: "20:30"}}, true},
		{"a deny is not an allow", []Rule{{Action: ActionDeny, Scope: ScopeHost, Ports: "22"}}, false},
		// A routed rule is about traffic to a container. Nothing about
		// it lets anybody into the host, and counting it would let the
		// lockout through the one door built to stop it.
		{"a container rule is not the host", []Rule{{Action: ActionAllow, Scope: ScopeApps, Ports: "22"}}, false},
		{"another port entirely", []Rule{{Action: ActionAllow, Scope: ScopeHost, Ports: "2222"}}, false},
		{"nothing at all", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowsAny(tt.rules, ScopeHost, []int{22}); got != tt.want {
				t.Errorf("allowsAny = %v, want %v", got, tt.want)
			}
		})
	}
}

// A host that says nothing about sshd says nothing, and is not filled in
// with 22.
//
// The guess was harmless while it only decided whether to refuse. It
// stopped being harmless when enabling started opening these ports: 22
// on a host listening on 2222 opens a port nobody uses and then enables,
// which is the lockout arriving through the door built to stop it.
func TestAHostThatSaysNothingAboutSSHIsNotGuessedAt(t *testing.T) {
	if got := parsePorts(""); len(got) != 0 {
		t.Errorf("parsePorts(\"\") = %v, want nothing", got)
	}
	if got := parsePorts("2222\n22\n"); len(got) != 2 {
		t.Errorf("parsePorts read %v", got)
	}
}

// Nothing a caller sends reaches a command line without matching a
// pattern first. These are the values that must never get through.
func TestARuleThatWouldNotBeARule(t *testing.T) {
	valid := Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22"}
	if err := valid.Check(); err != nil {
		t.Fatalf("a plain rule was refused: %v", err)
	}

	for _, tt := range []struct {
		name string
		spec Spec
	}{
		{"a port that is a command", Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22; rm -rf /"}},
		{"a source that is a command", Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22", From: "$(id)"}},
		{"a comment with a newline", Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22", Comment: "a\nb"}},
		{"an unknown action", Spec{Scope: ScopeHost, Action: "drop", Port: "22"}},
		{"an unknown scope", Spec{Scope: "everything", Action: ActionAllow, Port: "22"}},
		{"a protocol that is not one", Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22", Protocol: "sctp"}},
		{"no port", Spec{Scope: ScopeHost, Action: ActionAllow}},
		// UFW cannot write a range into both tables at once, and its
		// own refusal is a usage message nobody reads as being about
		// this.
		{"a range with no protocol", Spec{Scope: ScopeHost, Action: ActionAllow, Port: "15000:15999"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.spec.Check(); err == nil {
				t.Errorf("%+v was accepted", tt.spec)
			}
		})
	}
}

// The two commands are different programs as far as UFW is concerned,
// and which one runs is the whole of whether a published container port
// is governed.
func TestTheCommandEachScopeBuilds(t *testing.T) {
	host := Spec{Scope: ScopeHost, Action: ActionAllow, Protocol: ProtocolTCP, Port: "22"}.Args()
	if strings.Join(host, " ") != "ufw allow proto tcp from any to any port 22" {
		t.Errorf("host rule: %v", host)
	}

	apps := Spec{
		Scope: ScopeApps, Action: ActionAllow, Protocol: ProtocolTCP,
		Port: "15432", From: "10.0.0.0/8", Comment: "analytics",
	}.Args()
	want := "ufw route allow proto tcp from 10.0.0.0/8 to any port 15432 comment analytics"
	if strings.Join(apps, " ") != want {
		t.Errorf("apps rule: %v", apps)
	}
}

// A firewall that is off reports no rules at all — `ufw status` answers
// "inactive" and stops — while the rules somebody added sit in its file
// waiting to be applied.
//
// Reading only that listing was a dead end with no way out: adding the
// rule for SSH did nothing visible, so the check before enabling never
// saw it, so enabling stayed refused however many times you added it.
// `ufw show added` is what answers while it is off, and it prints
// commands rather than a table.
func TestTheRulesAFirewallThatIsOffStillHas(t *testing.T) {
	rules := parseAdded(`Added user rules (see 'ufw status' for running firewall):
ufw allow 22/tcp
ufw route allow proto tcp from any to any port 15432 comment cubeship
ufw deny 5432
`)
	if len(rules) != 3 {
		t.Fatalf("read %d rules: %+v", len(rules), rules)
	}

	// The shorthand somebody types by hand.
	if rules[0].Action != ActionAllow || rules[0].Ports != "22" ||
		rules[0].Protocol != ProtocolTCP || rules[0].Scope != ScopeHost {
		t.Errorf("the SSH rule read as %+v", rules[0])
	}
	// And the long form this daemon writes.
	if rules[1].Scope != ScopeApps || rules[1].Ports != "15432" ||
		rules[1].Comment != "cubeship" {
		t.Errorf("the routed rule read as %+v", rules[1])
	}
	if rules[2].Action != ActionDeny || rules[2].Ports != "5432" {
		t.Errorf("the deny read as %+v", rules[2])
	}

	// Which is what makes enabling possible at all.
	if !allowsAny(rules, ScopeHost, []int{22}) {
		t.Error("a rule that admits SSH was not seen, which is the dead end this fixes")
	}

	// There are no positions while it is off, so each rule carries the
	// command that removes it — "delete" where ufw wants it, which is
	// after "route" for a routed rule and before the verb otherwise.
	if got := strings.Join(rules[0].Delete, " "); got != "ufw delete allow 22/tcp" {
		t.Errorf("the delete for a host rule is %q", got)
	}
	if got := strings.Join(rules[1].Delete, " "); !strings.HasPrefix(got, "ufw route delete allow") {
		t.Errorf("the delete for a routed rule is %q", got)
	}
}
