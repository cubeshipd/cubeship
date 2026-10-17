package firewall

import (
	"context"
	"strconv"
	"strings"
	"sync"

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

// Exposed answers which host ports this instance has deliberately published.
type Exposed interface {
	ExposedPorts(ctx context.Context) ([]int, error)
}

// afterRules is the file ufw loads after its own rules, and the one the
// stanza lives in.
const afterRules = "/etc/ufw/after.rules"

// A firewall is the instance's own wiring, and reading it is as
// sensitive as writing it: the list of what is open is exactly what
// somebody probing would like to have.
const manageRole = user.RoleAdmin

// Service is the use cases. It holds no repository because it owns no
// rows — see the package comment.
type Service struct {
	host    Host
	ports   Ports
	exposed Exposed
	// dataDir is the one path this module writes to, and the trick that
	// makes writing to the host possible at all: it is mounted at the
	// same path inside and out, so a file the daemon writes is a file
	// the host can read. See AdoptDocker.
	dataDir string
	// rules is afterRules; a test points it at a file of its own.
	rules string
	// mu serializes everything that writes the stanza: two exposes at
	// once would otherwise race on one file.
	mu sync.Mutex
}

func NewService(host Host, ports Ports, exposed Exposed, dataDir string) *Service {
	return &Service{host: host, ports: ports, exposed: exposed, dataDir: dataDir, rules: afterRules}
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
echo '` + statusSeparator + `'
sed -n '/` + dockerBeginMarker + `/,/` + dockerEndMarker + `/p' /etc/ufw/after.rules 2>/dev/null | grep -e '--ctorigdstport' || true
`

const noUFW = "cubeship: no ufw"

// Status reads the host's firewall.
func (s *Service) Status(ctx context.Context, caller *user.User) (*Status, error) {
	if err := user.Allow(caller, user.ResFirewall, user.LevelView, ""); err != nil {
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
	for len(sections) < 6 {
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

	// What the host's stanza admits by published port, read off the file
	// rather than the database: the screen says what the host has.
	// What is actually exposed on a Docker host, which is rarely what
	// the host's own services are listening on.
	if s.ports != nil {
		published, err := s.ports.PublishedPorts(ctx)
		if err == nil {
			for _, p := range published {
				out := Published{
					Port: p.Port, Inside: p.Inside,
					Protocol: p.Protocol, Container: p.Container,
				}
				// A published port is admitted by a *routed* rule, and
				// by one naming the port the container is on rather
				// than the one somebody typed. A host rule for either
				// number governs traffic that never reaches this
				// container.
				switch {
				case allowsAny(status.Rules, ScopeApps, []int{out.Port}):
					out.Allowed, out.AllowedBy = true, AllowedByRule
				case parseExposedRules(sections[5])[out.Port]:
					out.Allowed, out.AllowedBy = true, AllowedByExposed
				}
				status.Published = append(status.Published, out)
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
		if r.Action != ActionAllow || r.Scope != scope || r.V6 {
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
	if err := user.Allow(caller, user.ResFirewall, user.LevelManage, ""); err != nil {
		return nil, err
	}
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
	if err := user.Allow(caller, user.ResFirewall, user.LevelManage, ""); err != nil {
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
