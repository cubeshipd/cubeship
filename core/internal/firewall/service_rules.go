package firewall

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"cubeship/internal/platform/hostexec"
	"cubeship/internal/user"
)

// Specs is the rules this request comes to.
//
// `inside` is what the published port becomes once Docker has
// translated it, and is empty when the two are the same or when the
// port is not published at all. See Spec.Inside: a forwarded rule is
// consulted after the translation, so it is the only number that can
// match.
func (r Request) Specs(inside string) []Spec {
	one := func(from string) Spec {
		return Spec{
			Scope: r.Scope, Action: r.Action, Protocol: r.Protocol,
			Port: r.Port, Inside: inside, From: from, Comment: r.Comment,
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

// insideOf is the container port behind a published one, or empty when
// nothing publishes it or the two are the same.
//
// Empty rather than the same number, so a rule about a port nothing
// publishes — one written ahead of the thing it is for — comes out
// exactly as it did before this existed.
// admits is the port a forwarded rule has to name to let this one
// through: the container's, whenever Docker is translating.
func admits(p Published) int {
	if p.Inside != 0 {
		return p.Inside
	}
	return p.Port
}

func insideOf(published []Published, port string) string {
	n, err := strconv.Atoi(port)
	if err != nil {
		// A range. Nothing published maps a range, and guessing at one
		// would be writing a rule for ports nobody asked about.
		return ""
	}
	for _, p := range published {
		if p.Port == n && p.Inside != 0 && p.Inside != p.Port {
			return strconv.Itoa(p.Inside)
		}
	}
	return ""
}

// AddRule writes the rules a request comes to.
//
// Every one is checked before any is written. A request half applied is
// a firewall nobody asked for, and the half that landed is the half
// somebody then has to find.
func (s *Service) AddRule(ctx context.Context, caller *user.User, req Request) (*Status, error) {
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
	// **The number somebody types is the one they can see**, which is
	// the published port — and the rule has to be written for the one
	// the packet carries by the time the forward chain looks at it.
	// Asking them for the second would be asking them to know how
	// Docker translates a port.
	specs := req.Specs("")
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
	if req.Scope == ScopeApps {
		if err := s.syncPublished(ctx); err != nil {
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
	if err := user.Allow(caller, user.ResFirewall, user.LevelManage, ""); err != nil {
		return nil, err
	}
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

	specs := req.Specs("")
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
	if req.Scope == ScopeApps || found.Scope == ScopeApps {
		if err := s.syncPublished(ctx); err != nil {
			return nil, err
		}
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
	if err := user.Allow(caller, user.ResFirewall, user.LevelManage, ""); err != nil {
		return nil, err
	}
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
	if found.Scope == ScopeApps {
		if err := s.syncPublished(ctx); err != nil {
			return nil, err
		}
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
// Everything about this is in renderDockerBlock and the package comment. What
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
	if status.DockerAdopted {
		return status, nil
	}
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()

	// Read before anything is written, so a failure here changes nothing.
	ports, err := s.exposedPorts(ctx)
	if err != nil {
		return nil, err
	}
	block, err := renderDockerBlock(ports)
	if err != nil {
		return nil, err
	}
	inStanza := map[int]bool{}
	for _, p := range ports {
		inStanza[p] = true
	}

	for _, port := range append([]int{80, 443}, allow...) {
		// The stanza returns this one by the port it was published on. A
		// rule for it would name the port inside the container, which
		// opens every database of the same engine.
		if inStanza[port] {
			continue
		}
		spec := Spec{
			Scope: ScopeApps, Action: ActionAllow, Protocol: ProtocolTCP,
			Port:    strconv.Itoa(port),
			Inside:  insideOf(status.Published, strconv.Itoa(port)),
			Comment: "cubeship",
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
	// The leading newline is for a file whose last line has none.
	if err := os.WriteFile(path, []byte("\n"+block), 0o600); err != nil {
		return nil, fmt.Errorf("write the docker stanza: %w", err)
	}
	if err := s.script(ctx, adoptScript(path)); err != nil {
		return nil, err
	}
	s.mu.Unlock()
	locked = false
	if err := s.SyncPublished(ctx); err != nil {
		return nil, err
	}
	return s.Status(ctx, caller)
}
