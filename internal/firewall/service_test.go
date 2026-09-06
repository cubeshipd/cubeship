package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/user"
)

// fakeHost is a machine that answers whatever the test says, and
// remembers what it was asked to do.
//
// Every test here is about a refusal or an ordering, and both are
// decided before anything reaches the host — so a fake is not a
// simplification, it is the only way to assert that the dangerous
// command was never sent.
type fakeHost struct {
	status string
	// ran is every command, in order. The order is the assertion in
	// TestAdoptingAllowsBeforeItDenies.
	ran  []string
	fail map[string]string
}

func (f *fakeHost) Available() bool { return true }

func (f *fakeHost) Run(_ context.Context, argv ...string) (hostexec.Result, error) {
	line := strings.Join(argv, " ")
	f.ran = append(f.ran, line)
	if msg, bad := f.fail[argv[0]]; bad {
		return hostexec.Result{Output: msg, Code: 1}, nil
	}
	return hostexec.Result{}, nil
}

func (f *fakeHost) Script(_ context.Context, line string) (hostexec.Result, error) {
	if strings.Contains(line, "ufw status verbose") {
		return hostexec.Result{Output: f.status}, nil
	}
	f.ran = append(f.ran, "script")
	return hostexec.Result{}, nil
}

// status builds what the read script prints: the five sections, in
// order, separated the way the script separates them.
//
// `added` is what ufw reports while it is off, and it is a separate
// section because it is a separate command with a different shape — see
// parseAdded.
func status(verbose, numbered, added, adopted, sshPorts string) string {
	return strings.Join([]string{verbose, numbered, added, adopted, sshPorts}, statusSeparator)
}

const active = "Status: active\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nufw status verbose"

var admin = &user.User{Username: "admin", Role: user.RoleAdmin}

func newService(t *testing.T, h *fakeHost) *Service {
	t.Helper()
	return NewService(h, nil, t.TempDir())
}

// The guarantee this module exists for: the firewall is never turned on
// in a way that ends the session it was asked from.
//
// UFW denies incoming by default, so enabling it with nothing admitting
// SSH costs somebody the machine. Enabling therefore writes that rule
// itself — refusing was the first answer, and it was a mechanical step
// in front of a button for a rule the daemon already knew how to write.
func TestEnablingAdmitsSSHBeforeItTurnsAnythingOn(t *testing.T) {
	host := &fakeHost{status: status(active, "", "", "0", "22")}
	if _, err := newService(t, host).Enable(context.Background(), admin); err != nil {
		t.Fatalf("enable: %v", err)
	}

	if len(host.ran) < 2 {
		t.Fatalf("ran %v", host.ran)
	}
	if !strings.Contains(host.ran[0], "port 22") {
		t.Errorf("the first thing it did was not admitting SSH: %v", host.ran)
	}
	if !strings.Contains(host.ran[len(host.ran)-1], "enable") {
		t.Errorf("the last thing it did was not enabling: %v", host.ran)
	}
}

// And it follows where sshd actually listens, because a rule for the
// wrong port is the lockout wearing the safeguard's clothes.
func TestEnablingAdmitsThePortSSHIsReallyOn(t *testing.T) {
	host := &fakeHost{status: status(active, "", "", "0", "2222")}
	if _, err := newService(t, host).Enable(context.Background(), admin); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !strings.Contains(host.ran[0], "port 2222") {
		t.Errorf("it opened the wrong port: %v", host.ran)
	}
}

// The one case that cannot be made safe, and the one refusal left: a
// host whose sshd did not say. Any rule written there would be a guess,
// and a wrong guess is the lockout.
func TestEnablingIsRefusedWhenNobodyKnowsWhereSSHIs(t *testing.T) {
	host := &fakeHost{status: status(active, "", "", "0", "")}
	_, err := newService(t, host).Enable(context.Background(), admin)

	if !errors.Is(err, ErrWouldLockYouOut) {
		t.Fatalf("enabling was allowed: %v", err)
	}
	for _, cmd := range host.ran {
		if strings.Contains(cmd, "enable") || strings.Contains(cmd, "allow") {
			t.Fatalf("it guessed anyway: %v", host.ran)
		}
	}
}

// And it goes through once something does.
func TestEnablingProceedsOnceSSHIsAdmitted(t *testing.T) {
	host := &fakeHost{status: status(active,
		"[ 1] 22/tcp                     ALLOW IN    Anywhere", "", "0", "22")}
	if _, err := newService(t, host).Enable(context.Background(), admin); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if len(host.ran) == 0 || !strings.Contains(host.ran[0], "ufw --force enable") {
		t.Errorf("ran %v", host.ran)
	}
}

// A rule about forwarded traffic, on a host where nothing forwards
// through UFW, is a line that governs nothing. Writing it would be the
// exact lie this module exists to avoid, so it is refused.
func TestAContainerRuleIsRefusedUntilDockerIsAdopted(t *testing.T) {
	host := &fakeHost{status: status(active,
		"", "", "0", "22")}
	_, err := newService(t, host).AddRule(context.Background(), admin, Spec{
		Scope: ScopeApps, Action: ActionAllow, Protocol: ProtocolTCP, Port: "15432",
	})
	if !errors.Is(err, ErrDockerNotAdopted) {
		t.Fatalf("the rule was written: %v", err)
	}

	// The same rule is fine once the stanza is in.
	host = &fakeHost{status: status(active,
		"", "", "1", "22")}
	if _, err := newService(t, host).AddRule(context.Background(), admin, Spec{
		Scope: ScopeApps, Action: ActionAllow, Protocol: ProtocolTCP, Port: "15432",
	}); err != nil {
		t.Fatalf("adopted, and still refused: %v", err)
	}
}

// The order is the whole safety of adopting.
//
// The stanza starts denying every published port nothing admits. The
// allow rules are inert until it lands, so they go first — the other way
// round is an instance off the internet for however long the second
// command takes. And 80 and 443 go in whatever was asked for: they are
// Traefik, which is every app and the dashboard the button was pressed
// from.
func TestAdoptingAllowsBeforeItDenies(t *testing.T) {
	host := &fakeHost{status: status(active,
		"", "", "0", "22")}
	if _, err := newService(t, host).AdoptDocker(context.Background(), admin, []int{15432}); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	var allowed []string
	stanza := -1
	for i, cmd := range host.ran {
		if cmd == "script" {
			stanza = i
			continue
		}
		allowed = append(allowed, cmd)
	}
	if stanza == -1 {
		t.Fatal("the stanza was never installed")
	}
	if stanza != len(host.ran)-1 {
		t.Errorf("the stanza went in before some rule: %v", host.ran)
	}
	for _, want := range []string{"port 80", "port 443", "port 15432"} {
		if !strings.Contains(strings.Join(allowed, "\n"), want) {
			t.Errorf("%s was not allowed first: %v", want, allowed)
		}
	}
	for _, cmd := range allowed {
		if !strings.HasPrefix(cmd, "ufw route allow") {
			t.Errorf("an allow was written as a host rule: %q", cmd)
		}
	}
}

// A daemon with no way onto the host reports a firewall it knows nothing
// about, rather than an error. That is `make dev` and every test that
// builds a server — and a screen that said "failed" there would be
// describing the developer's machine, not the instance.
func TestWithNoHostThereIsNothingToReport(t *testing.T) {
	svc := NewService(nil, nil, t.TempDir())
	status, err := svc.Status(context.Background(), admin)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Available || status.Installed || status.Enabled {
		t.Errorf("a firewall was reported with no host: %+v", status)
	}
	if _, err := svc.Enable(context.Background(), admin); !errors.Is(err, hostexec.ErrUnavailable) {
		t.Errorf("enabling answered %v", err)
	}
}

// A host with no ufw is a fact to report, not a failure. Plenty of
// machines have none, and installing one is the operator's call on their
// own package manager.
func TestAHostWithoutUFWSaysSo(t *testing.T) {
	host := &fakeHost{status: noUFW}
	status, err := newService(t, host).Status(context.Background(), admin)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Available || status.Installed {
		t.Errorf("%+v", status)
	}
	if _, err := newService(t, host).Enable(context.Background(), admin); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("enabling answered %v", err)
	}
}

// Reading which ports are open is as sensitive as changing them: it is
// the map of the instance, and exactly what somebody probing would like.
func TestOnlyAnAdminSeesTheFirewall(t *testing.T) {
	svc := newService(t, &fakeHost{status: status(active,
		"", "", "0", "22")})
	member := &user.User{Username: "member", Role: user.RoleMember}

	for _, call := range []struct {
		name string
		err  error
	}{
		{"status", func() error { _, err := svc.Status(context.Background(), member); return err }()},
		{"enable", func() error { _, err := svc.Enable(context.Background(), member); return err }()},
		{"disable", func() error { _, err := svc.Disable(context.Background(), member); return err }()},
		{"add", func() error {
			_, err := svc.AddRule(context.Background(), member, Spec{Scope: ScopeHost, Action: ActionAllow, Port: "22"})
			return err
		}()},
		{"delete", func() error { _, err := svc.DeleteRule(context.Background(), member, 1, ""); return err }()},
		{"adopt", func() error { _, err := svc.AdoptDocker(context.Background(), member, nil); return err }()},
		{"release", func() error { _, err := svc.ReleaseDocker(context.Background(), member); return err }()},
	} {
		if !errors.Is(call.err, user.ErrForbidden) {
			t.Errorf("a member could %s: %v", call.name, call.err)
		}
	}
}

// Deleting is by position, and positions shift. A screen acting on a
// listing that has moved would delete a different rule, and the only
// sign would be a port that stopped answering.
func TestDeletingRefusesWhenTheListMoved(t *testing.T) {
	host := &fakeHost{status: status(active,
		"[ 1] 22/tcp                     ALLOW IN    Anywhere", "", "0", "22")}
	svc := newService(t, host)

	_, err := svc.DeleteRule(context.Background(), admin, 1, "80/tcp ALLOW IN Anywhere")
	if !errors.Is(err, ErrRuleChanged) {
		t.Fatalf("it deleted whatever was there: %v", err)
	}
	if _, err := svc.DeleteRule(context.Background(), admin, 9, ""); !errors.Is(err, ErrNoSuchRule) {
		t.Errorf("deleting nothing answered %v", err)
	}

	// The same call with what is actually there goes through, however
	// UFW padded the columns.
	if _, err := svc.DeleteRule(context.Background(), admin, 1, "22/tcp   ALLOW IN   Anywhere"); err != nil {
		t.Errorf("a correct delete was refused: %v", err)
	}
}

// The published ports are what a firewall on a Docker host is about, so
// the report carries them and says which are already admitted.
func TestTheReportSaysWhichPublishedPortsAreOpen(t *testing.T) {
	host := &fakeHost{status: status(active,
		"[ 1] 80/tcp                     ALLOW FWD   Anywhere", "", "1", "22")}
	svc := NewService(host, fakePorts{
		{Port: 80, Protocol: "tcp", Container: "cubeship-traefik"},
		{Port: 15432, Protocol: "tcp", Container: "cubeship-db-pg"},
	}, t.TempDir())

	got, err := svc.Status(context.Background(), admin)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(got.Published) != 2 {
		t.Fatalf("published: %+v", got.Published)
	}
	// A routed allow is what admits a published port — a host rule for
	// the same number would not.
	if !got.Published[0].Allowed {
		t.Errorf("80 is allowed and reported closed: %+v", got.Published[0])
	}
	if got.Published[1].Allowed {
		t.Errorf("15432 is not allowed and reported open: %+v", got.Published[1])
	}
}

type fakePorts []dockerx.PublishedPort

func (f fakePorts) PublishedPorts(context.Context) ([]dockerx.PublishedPort, error) {
	return f, nil
}

// The dead end, end to end.
//
// A firewall that is off reports no rules, so the check before enabling
// saw nothing however many times somebody added the rule for SSH — and
// enabling stayed refused forever. The added rules are what answers.
func TestARuleAddedWhileItIsOffIsStillARule(t *testing.T) {
	const off = "Status: inactive\nufw status verbose"
	host := &fakeHost{status: status(off, "",
		"Added user rules (see 'ufw status' for running firewall):\nufw allow 22/tcp",
		"0", "22")}
	svc := newService(t, host)

	got, err := svc.Status(context.Background(), admin)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got.Enabled {
		t.Fatal("an inactive firewall read as on")
	}
	if len(got.Rules) != 1 || !got.SSHAllowed {
		t.Fatalf("the rule waiting to be applied was not seen: %+v", got.Rules)
	}

	if _, err := svc.Enable(context.Background(), admin); err != nil {
		t.Fatalf("enabling was still refused: %v", err)
	}

	// And it can be removed by its own spelling, since there is no
	// position to remove it by.
	if _, err := svc.DeleteRule(context.Background(), admin, 1, ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var deleted bool
	for _, cmd := range host.ran {
		if cmd == "ufw --force delete allow 22/tcp" {
			deleted = true
		}
		if strings.HasSuffix(cmd, "delete 1") {
			t.Errorf("it deleted by a position that does not exist: %q", cmd)
		}
	}
	if !deleted {
		t.Errorf("ran %v", host.ran)
	}
}
