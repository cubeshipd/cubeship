package firewall

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/user"
)

// Host is what runs a command on the machine this instance is on.
//
// An interface rather than the concrete runner, for the same reason
// certificates.Engine is one: a test builds a whole server, and a
// server that reached the host would be a test that edited the
// developer's own firewall. Nil is a service that answers "not
// available", which is exactly what a test and `make dev` should see.
type Host interface {
	Available() bool
	Run(ctx context.Context, argv ...string) (hostexec.Result, error)
	Script(ctx context.Context, line string) (hostexec.Result, error)
}

// Ports answers which host ports containers are publishing — the thing
// a firewall on a Docker host is actually about.
type Ports interface {
	PublishedPorts(ctx context.Context) ([]dockerx.PublishedPort, error)
}

// A firewall is the instance's own wiring, and reading it is as
// sensitive as writing it: the list of what is open is exactly what
// somebody probing would like to have.
const manageRole = user.RoleAdmin

// Service is the use cases. It holds no repository because it owns no
// rows — see the package comment.
type Service struct {
	host  Host
	ports Ports
	// dataDir is the one path this module writes to, and the trick that
	// makes writing to the host possible at all: it is mounted at the
	// same path inside and out, so a file the daemon writes is a file
	// the host can read. See AdoptDocker.
	dataDir string
}

func NewService(host Host, ports Ports, dataDir string) *Service {
	return &Service{host: host, ports: ports, dataDir: dataDir}
}

// reachable reports whether there is a host to talk to at all.
func (s *Service) reachable() bool { return s.host != nil && s.host.Available() }

// readStatus is one container's worth of everything the screen needs.
//
// One script rather than five calls: each is a container create, start,
// wait and remove, and this page is read every time somebody opens it.
// The sections are split on a marker the script prints, which is why it
// cannot appear in any of the output being split.
const readStatus = `
if ! command -v ufw >/dev/null 2>&1; then echo '` + noUFW + `'; exit 0; fi
ufw status verbose
echo '` + statusSeparator + `'
ufw status numbered
echo '` + statusSeparator + `'
ufw show added
echo '` + statusSeparator + `'
grep -c '` + dockerBeginMarker + `' /etc/ufw/after.rules 2>/dev/null || true
echo '` + statusSeparator + `'
sshd -T 2>/dev/null | awk '/^port /{print $2}' || true
`

const noUFW = "cubeship: no ufw"

// Status reads the host's firewall.
func (s *Service) Status(ctx context.Context, caller *user.User) (*Status, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	status := &Status{Rules: []Rule{}}

	if !s.reachable() {
		return status, nil
	}
	res, err := s.host.Script(ctx, readStatus)
	if err != nil {
		// Not reachable is a fact about this daemon, not a failure of
		// the request: `make dev` runs on the host and cannot do this.
		return status, nil
	}
	status.Available = true
	if strings.Contains(res.Output, noUFW) {
		return status, nil
	}
	status.Installed = true

	sections := strings.Split(res.Output, statusSeparator)
	for len(sections) < 5 {
		sections = append(sections, "")
	}
	status.Enabled, status.DefaultIncoming, status.Rules = parseStatus(
		sections[0] + statusSeparator + sections[1])

	// A firewall that is off reports no rules at all — `ufw status`
	// answers "inactive" and stops — while the rules somebody added are
	// sitting in its file waiting to be applied.
	//
	// Reading only that listing was a dead end you could not get out
	// of: adding the rule for SSH did nothing visible, so the check
	// before enabling never saw it, so enabling stayed refused however
	// many times you added it. `ufw show added` is what answers while
	// it is off.
	if !status.Enabled {
		status.Rules = parseAdded(sections[2])
	}
	if status.Rules == nil {
		status.Rules = []Rule{}
	}
	status.DockerAdopted = strings.TrimSpace(sections[3]) != "" &&
		strings.TrimSpace(sections[3]) != "0"
	status.SSHPorts = parsePorts(sections[4])
	status.SSHAllowed = allowsAny(status.Rules, ScopeHost, status.SSHPorts)
	markProtected(status)

	// What is actually exposed on a Docker host, which is rarely what
	// the host's own services are listening on.
	if s.ports != nil {
		published, err := s.ports.PublishedPorts(ctx)
		if err == nil {
			for _, p := range published {
				status.Published = append(status.Published, Published{
					Port: p.Port, Protocol: p.Protocol, Container: p.Container,
					// A published port is admitted by a *routed* rule.
					// A host rule for the same number governs traffic
					// that never reaches this container.
					Allowed: allowsAny(status.Rules, ScopeApps, []int{p.Port}),
				})
			}
		}
	}
	return status, nil
}

// parsePorts reads the numbers sshd printed, one per line.
//
// **Empty means "could not tell", and it is not filled in with 22.** It
// used to be, back when the only thing this decided was whether to
// refuse — a guess is harmless when the consequence is refusing. It is
// not harmless now that Enable opens these ports itself: guessing 22 on
// a host listening on 2222 would open a port nobody uses, enable, and
// lock the operator out through the one door built to stop exactly
// that.
func parsePorts(out string) []int {
	var ports []int
	for _, line := range strings.Fields(out) {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n > 0 && n < 65536 {
			ports = append(ports, n)
		}
	}
	return ports
}

// allowsAny reports whether some rule of that scope admits one of these
// ports.
//
// **The scope is not optional.** A routed rule admits traffic to a
// container and nothing else; a host rule admits traffic to the machine
// and nothing else. Asking without saying which would answer "SSH is
// allowed" for a rule that only opens a database's published port, and
// that answer is the one standing between an operator and a locked
// machine.
//
// Deliberately generous about *how*: a rule for the port, a range that
// contains it, or a comma list that names it all count. Being strict
// there would mean refusing to enable a firewall that is in fact safe,
// with no way for the operator to say otherwise.
func allowsAny(rules []Rule, scope Scope, ports []int) bool {
	for _, r := range rules {
		if r.Action != ActionAllow || r.Scope != scope {
			continue
		}
		for _, p := range ports {
			if coversPort(r.Ports, p) {
				return true
			}
		}
	}
	return false
}

func coversPort(spec string, port int) bool {
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if low, high, isRange := strings.Cut(part, ":"); isRange {
			lo, err1 := strconv.Atoi(strings.TrimSpace(low))
			hi, err2 := strconv.Atoi(strings.TrimSpace(high))
			if err1 == nil && err2 == nil && port >= lo && port <= hi {
				return true
			}
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n == port {
			return true
		}
	}
	return false
}

// markProtected marks the rules this screen will not delete.
//
// Every host rule admitting a port sshd is on, and only while the
// firewall is running — with it off, removing one cannot lock anybody
// out, and Enable would write it again anyway.
//
// Every one of them rather than "the last one", deliberately. Somebody
// moving SSH from 22 to 2222 does not have to fight the screen for it:
// they add the new rule, move sshd, and the old one stops being an SSH
// rule on the next read, because what counts is where sshd is *now*.
func markProtected(status *Status) {
	if !status.Enabled {
		return
	}
	for i := range status.Rules {
		r := &status.Rules[i]
		if r.Action == ActionAllow && r.Scope == ScopeHost &&
			allowsAny([]Rule{*r}, ScopeHost, status.SSHPorts) {
			r.Protected = true
		}
	}
}

// Enable turns the firewall on, and never turns it on in a way that
// ends the session it was asked from.
//
// UFW denies incoming by default, so enabling it on a machine reached
// over SSH with no rule for SSH is the classic way to lose a VPS: it is
// silent, and the person it happens to is by definition no longer able
// to undo it.
//
// **So it admits SSH itself rather than refusing.** Refusing was the
// first answer and it was the wrong shape of one: if a rule is
// compulsory, making somebody add it by hand is a mechanical step in
// front of a button, and one they can get wrong — the daemon already
// knows the port and can write it correctly. What is compulsory is that
// the firewall is never enabled without it, and adding it keeps that
// promise better than refusing does.
//
// The refusal survives for the one case where the guarantee cannot be
// kept: a host whose sshd did not say which port it is on. There, any
// rule this wrote would be a guess, and a wrong guess is the lockout.
func (s *Service) Enable(ctx context.Context, caller *user.User) (*Status, error) {
	status, err := s.Status(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !status.Available {
		return nil, hostexec.ErrUnavailable
	}
	if !status.Installed {
		return nil, ErrNotInstalled
	}
	if !status.SSHAllowed {
		if len(status.SSHPorts) == 0 {
			return nil, ErrSSHUnknown
		}
		for _, port := range status.SSHPorts {
			spec := Spec{
				Scope: ScopeHost, Action: ActionAllow, Protocol: ProtocolTCP,
				Port: strconv.Itoa(port), Comment: "ssh",
			}
			if err := s.run(ctx, spec.Args()...); err != nil {
				return nil, err
			}
		}
	}
	// --force is not "do it anyway": it is what answers the y/n prompt
	// ufw would otherwise wait on forever with no terminal attached.
	if err := s.run(ctx, "ufw", "--force", "enable"); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

// Disable turns it off. No refusal: switching a firewall off cannot
// lock anybody out, and an operator who wants it off in a hurry is
// usually having a bad enough day already.
func (s *Service) Disable(ctx context.Context, caller *user.User) (*Status, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if err := s.run(ctx, "ufw", "disable"); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

// Request is a rule as a screen asks for it, which is not always one
// rule.
//
// UFW takes one source per rule — there is no "from A or B" — so
// admitting a port from three addresses is three rules. That is the
// tool's shape, not a limitation worth hiding, but it should not make
// the *asking* three times either: this is one act, and it either
// happens or it does not.
type Request struct {
	Scope    Scope
	Action   Action
	Protocol Protocol
	Port     string
	// Sources are the addresses it applies from. Empty — or any entry
	// that is empty — means anywhere, and anywhere makes the others
	// meaningless, so it wins.
	Sources []string
	Comment string
}

// Specs is the rules this request comes to.
func (r Request) Specs() []Spec {
	one := func(from string) Spec {
		return Spec{
			Scope: r.Scope, Action: r.Action, Protocol: r.Protocol,
			Port: r.Port, From: from, Comment: r.Comment,
		}
	}
	if len(r.Sources) == 0 {
		return []Spec{one("")}
	}

	seen := map[string]bool{}
	specs := make([]Spec, 0, len(r.Sources))
	for _, from := range r.Sources {
		if strings.TrimSpace(from) == "" {
			// Anywhere. Nothing narrower adds anything to it.
			return []Spec{one("")}
		}
		if seen[from] {
			continue
		}
		seen[from] = true
		specs = append(specs, one(from))
	}
	return specs
}

// AddRule writes the rules a request comes to.
//
// Every one is checked before any is written. A request half applied is
// a firewall nobody asked for, and the half that landed is the half
// somebody then has to find.
func (s *Service) AddRule(ctx context.Context, caller *user.User, req Request) (*Status, error) {
	status, err := s.Status(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !status.Available {
		return nil, hostexec.ErrUnavailable
	}
	if !status.Installed {
		return nil, ErrNotInstalled
	}
	specs := req.Specs()
	for _, spec := range specs {
		if err := spec.Check(); err != nil {
			return nil, err
		}
	}
	// A rule about forwarded traffic is a rule about a chain that does
	// not exist yet. Writing it would put a line on the screen that
	// governs nothing — which is the one failure this module is built
	// to avoid, so it is refused rather than accepted and explained.
	if req.Scope == ScopeApps && !status.DockerAdopted {
		return nil, fmt.Errorf("%w: turn on Docker port control first, or this rule would sit in the table doing nothing", ErrDockerNotAdopted)
	}
	for _, spec := range specs {
		if err := s.run(ctx, spec.Args()...); err != nil {
			return nil, err
		}
	}
	return s.Status(ctx, caller)
}

// ReplaceRule edits a rule, which UFW has no way to do: it is a delete
// and an add, and the interesting part is the order of the two.
//
// **The new rule goes in first, at the old one's position, and the old
// one is removed after.** The other way round has a window where the
// rule is simply gone, and if the add then fails — the host refuses it,
// the daemon dies — what is left is a firewall missing a line nobody
// took out on purpose. This way the window holds a duplicate, which is
// harmless and visible.
//
// The position is kept because order is meaning here: the first rule
// that matches decides, so a rule appended to the end is a rule that may
// now be shadowed by one above it.
func (s *Service) ReplaceRule(ctx context.Context, caller *user.User, index int, expect string, req Request) (*Status, error) {
	status, err := s.Status(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !status.Installed {
		return nil, ErrNotInstalled
	}
	found, err := findRule(status, index, expect)
	if err != nil {
		return nil, err
	}
	// The same refusal as deleting, and for the same reason: an edit
	// that changed the source of the rule admitting SSH is a lockout
	// with an extra step.
	if found.Protected {
		return nil, KeepsYouInError(*found)
	}

	specs := req.Specs()
	for _, spec := range specs {
		if err := spec.Check(); err != nil {
			return nil, err
		}
	}
	if req.Scope == ScopeApps && !status.DockerAdopted {
		return nil, fmt.Errorf("%w: turn on Docker port control first, or this rule would sit in the table doing nothing", ErrDockerNotAdopted)
	}

	for i, spec := range specs {
		if status.Enabled {
			if err := s.run(ctx, spec.InsertArgs(index+i)...); err != nil {
				return nil, err
			}
			continue
		}
		// With the firewall off there are no positions to insert at —
		// see parseAdded — so the new rules are appended and the old one
		// removed by its own spelling. Nothing is ordered yet anyway.
		if err := s.run(ctx, spec.Args()...); err != nil {
			return nil, err
		}
	}

	argv := []string{"ufw", "--force", "delete", strconv.Itoa(index + len(specs))}
	if len(found.Delete) > 0 {
		argv = append([]string{"ufw", "--force"}, found.Delete[1:]...)
	}
	if err := s.run(ctx, argv...); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

// findRule is the lookup both editing and deleting start with: the rule
// at a position, and a refusal when the list has moved under the caller.
func findRule(status *Status, index int, expect string) (*Rule, error) {
	for i := range status.Rules {
		if status.Rules[i].Index != index {
			continue
		}
		found := &status.Rules[i]
		if expect != "" && !sameRule(found.Text, expect) {
			return nil, ErrRuleChanged
		}
		return found, nil
	}
	return nil, ErrNoSuchRule
}

// DeleteRule removes the rule at index.
//
// UFW deletes by position, and positions shift as soon as anything above
// them goes — so the caller sends what it believes is there, and this
// refuses if the list has moved under it. Without that, a stale screen
// deletes the wrong rule and the only sign is a port that stopped
// answering.
func (s *Service) DeleteRule(ctx context.Context, caller *user.User, index int, expect string) (*Status, error) {
	status, err := s.Status(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !status.Installed {
		return nil, ErrNotInstalled
	}

	found, err := findRule(status, index, expect)
	if err != nil {
		return nil, err
	}
	if found.Protected {
		return nil, KeepsYouInError(*found)
	}

	// Two ways out, and which one applies is decided by whether the
	// firewall is running. Active, ufw deletes by position. Off, there
	// are no positions — `ufw status numbered` answers "inactive" and
	// nothing else — so the rule is removed by its own spelling, which
	// is what `ufw show added` gave us.
	argv := []string{"ufw", "--force", "delete", strconv.Itoa(index)}
	if len(found.Delete) > 0 {
		argv = append([]string{"ufw", "--force"}, found.Delete[1:]...)
	}
	if err := s.run(ctx, argv...); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

// sameRule compares two renderings of a rule, ignoring how UFW padded
// the columns.
func sameRule(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// AdoptDocker puts published container ports under UFW's control.
//
// Everything about this is in dockerBlock and the package comment. What
// matters here is the order, and it is not cosmetic: the allow rules go
// in **first**, while they are still inert, and the stanza that starts
// denying goes in last. The other way round is an instance that is off
// the internet for however long the second command takes.
//
// 80 and 443 are always allowed, whatever the caller asked for. They are
// not a preference — they are Traefik, which is every app on the
// instance and the dashboard itself, and an adopt that took those down
// would be a button that breaks the thing you pressed it from.
func (s *Service) AdoptDocker(ctx context.Context, caller *user.User, allow []int) (*Status, error) {
	status, err := s.Status(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !status.Available {
		return nil, hostexec.ErrUnavailable
	}
	if !status.Installed {
		return nil, ErrNotInstalled
	}
	if status.DockerAdopted {
		return status, nil
	}

	for _, port := range append([]int{80, 443}, allow...) {
		spec := Spec{
			Scope: ScopeApps, Action: ActionAllow, Protocol: ProtocolTCP,
			Port: strconv.Itoa(port), Comment: "cubeship",
		}
		if err := spec.Check(); err != nil {
			return nil, err
		}
		if err := s.run(ctx, spec.Args()...); err != nil {
			return nil, err
		}
	}

	// The stanza travels through the data directory rather than through
	// a shell argument. That directory is mounted at the same path
	// inside and out — the one property everything else about running
	// the daemon as a container depends on — so the host can read a
	// file the daemon just wrote, and nothing has to escape several
	// hundred bytes of iptables syntax past two parsers.
	path := s.dataDir + "/ufw-docker.rules"
	if err := os.WriteFile(path, []byte(dockerBlock), 0o600); err != nil {
		return nil, fmt.Errorf("write the docker stanza: %w", err)
	}
	if err := s.script(ctx, adoptScript(path)); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

// ReleaseDocker takes it back out, leaving UFW as it was.
//
// Published ports go back to being open regardless of what any rule
// says, which is Docker's own behaviour and the reason for all of this.
func (s *Service) ReleaseDocker(ctx context.Context, caller *user.User) (*Status, error) {
	if err := user.Require(caller, manageRole); err != nil {
		return nil, err
	}
	if err := s.script(ctx, releaseScript); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}

func adoptScript(path string) string {
	return `
set -e
if grep -q '` + dockerBeginMarker + `' /etc/ufw/after.rules; then exit 0; fi
cp /etc/ufw/after.rules /etc/ufw/after.rules.cubeship-backup
cat ` + path + ` >> /etc/ufw/after.rules
ufw status | grep -q '^Status: active' && ufw reload || true
`
}

// releaseScript removes everything between the markers, which is
// exactly what was added and nothing an operator wrote by hand.
const releaseScript = `
set -e
sed -i '/` + dockerBeginMarker + `/,/` + dockerEndMarker + `/d' /etc/ufw/after.rules
ufw status | grep -q '^Status: active' && ufw reload || true
`

// run executes a command on the host and turns a non-zero exit into an
// error carrying what the command actually said.
//
// UFW's refusals are sentences — "ERROR: Bad port" — and passing them
// through is the difference between a screen that explains itself and
// one that says the operation failed.
func (s *Service) run(ctx context.Context, argv ...string) error {
	if !s.reachable() {
		return hostexec.ErrUnavailable
	}
	res, err := s.host.Run(ctx, argv...)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s", firstLine(res.Output))
	}
	return nil
}

func (s *Service) script(ctx context.Context, line string) error {
	if !s.reachable() {
		return hostexec.ErrUnavailable
	}
	res, err := s.host.Script(ctx, line)
	if err != nil {
		return err
	}
	if !res.OK() {
		return fmt.Errorf("%s", firstLine(res.Output))
	}
	return nil
}

func firstLine(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return "the host refused, and said nothing about why"
	}
	if line, _, found := strings.Cut(out, "\n"); found {
		return line
	}
	return out
}
