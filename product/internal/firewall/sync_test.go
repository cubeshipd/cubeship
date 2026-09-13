package firewall

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cubeship/internal/platform/hostexec"
)

type fakeExposed struct {
	ports []int
	err   error
}

func (f fakeExposed) ExposedPorts(context.Context) ([]int, error) { return f.ports, f.err }

// shellHost runs a script with this machine's own sh, against files in a
// temporary directory and a ufw that only writes down what it was asked.
//
// The script that replaces the block is the dangerous part of all this,
// and a fake that records "script" says nothing about what it does to a
// file. Nothing in it needs root when the file is the test's own.
type shellHost struct {
	dir   string
	state string
}

func newShellHost(t *testing.T, state string) *shellHost {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	ufw := "#!/bin/sh\necho \"$*\" >> \"$UFW_LOG\"\nif [ \"$1\" = status ]; then echo \"Status: $UFW_STATE\"; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "ufw"), []byte(ufw), 0o755); err != nil {
		t.Fatal(err)
	}
	return &shellHost{dir: dir, state: state}
}

func (h *shellHost) Available() bool { return true }

func (h *shellHost) Run(context.Context, ...string) (hostexec.Result, error) {
	return hostexec.Result{}, errors.New("shellHost runs scripts only")
}

func (h *shellHost) Script(ctx context.Context, line string) (hostexec.Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", line)
	cmd.Env = append(os.Environ(),
		"PATH="+filepath.Join(h.dir, "bin")+":"+os.Getenv("PATH"),
		"UFW_LOG="+h.ufwLog(),
		"UFW_STATE="+h.state)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return hostexec.Result{Output: string(out), Code: exit.ExitCode()}, nil
	}
	return hostexec.Result{Output: string(out)}, err
}

func (h *shellHost) ufwLog() string { return filepath.Join(h.dir, "ufw.log") }

func (h *shellHost) ufwCalls(t *testing.T) string {
	t.Helper()
	out, err := os.ReadFile(h.ufwLog())
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// syncFixture is a Service whose after.rules is a file the test wrote.
func syncFixture(t *testing.T, host Host, exposed Exposed, rules string) (*Service, string) {
	t.Helper()
	svc := NewService(host, nil, exposed, t.TempDir())
	svc.rules = filepath.Join(t.TempDir(), "after.rules")
	if err := os.WriteFile(svc.rules, []byte(rules), 0o640); err != nil {
		t.Fatal(err)
	}
	return svc, svc.rules
}

// What adopting leaves: the operator's file, a blank line, the stanza.
func adopted(t *testing.T, operator string, ports ...int) string {
	t.Helper()
	block, err := renderDockerBlock(ports)
	if err != nil {
		t.Fatal(err)
	}
	return operator + "\n" + block
}

const operatorRules = "# the operator's own\n*filter\n:ufw-after-input - [0:0]\nCOMMIT\n"

// Without the stanza nothing is denied, so there is nothing to write —
// and writing it would be adopting Docker nobody asked for.
func TestSyncingWithNoStanzaChangesNothing(t *testing.T) {
	host := newShellHost(t, "active")
	svc, rules := syncFixture(t, host, fakeExposed{ports: []int{15002}}, operatorRules)

	if err := svc.SyncPublished(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got, _ := os.ReadFile(rules); string(got) != operatorRules {
		t.Errorf("the file changed:\n%s", got)
	}
	if calls := host.ufwCalls(t); calls != "" {
		t.Errorf("ufw was asked for %q", calls)
	}
}

// The block is replaced whole with what is exposed now, the lines around
// it are kept, the file keeps its mode, nothing is left behind, and a
// running firewall is reloaded.
func TestSyncingReplacesTheBlockAndReloadsARunningFirewall(t *testing.T) {
	host := newShellHost(t, "active")
	before := adopted(t, operatorRules, 15000) + "# added after adopting\n"
	svc, rules := syncFixture(t, host, fakeExposed{ports: []int{15002}}, before)

	if err := svc.SyncPublished(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := os.ReadFile(rules)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.HasPrefix(text, operatorRules) || !strings.Contains(text, "# added after adopting\n") {
		t.Errorf("the operator's lines were not kept:\n%s", text)
	}
	if strings.Count(text, dockerBeginMarker) != 1 || strings.Count(text, dockerEndMarker) != 1 {
		t.Errorf("not exactly one block:\n%s", text)
	}
	if !strings.Contains(text, "--ctorigdstport 15002 ") || strings.Contains(text, "--ctorigdstport 15000 ") {
		t.Errorf("the block is not the new set:\n%s", text)
	}
	if !strings.HasSuffix(text, dockerEndMarker+"\n") {
		t.Errorf("the file does not end with the block:\n%s", text)
	}
	if info, err := os.Stat(rules); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("mode after: %v %v", info.Mode(), err)
	}
	if left, _ := filepath.Glob(rules + ".cubeship.*"); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
	if calls := host.ufwCalls(t); !strings.Contains(calls, "reload") {
		t.Errorf("a running firewall was not reloaded: %q", calls)
	}

	// And again, with the same set: the same file, not one more blank
	// line per expose.
	if err := svc.SyncPublished(context.Background()); err != nil {
		t.Fatalf("sync again: %v", err)
	}
	if again, _ := os.ReadFile(rules); string(again) != text {
		t.Errorf("a second sync changed the file:\n%q\n%q", text, again)
	}
}

// A firewall that is off is not started by a reload.
func TestSyncingDoesNotReloadAFirewallThatIsOff(t *testing.T) {
	host := newShellHost(t, "inactive")
	svc, rules := syncFixture(t, host, fakeExposed{}, adopted(t, operatorRules, 15000))

	if err := svc.SyncPublished(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got, _ := os.ReadFile(rules); strings.Contains(string(got), "conntrack") {
		t.Errorf("the unexposed port is still in the block:\n%s", got)
	}
	if calls := host.ufwCalls(t); strings.Contains(calls, "reload") {
		t.Errorf("an inactive firewall was reloaded: %q", calls)
	}
}

// A begin marker with no end is a file somebody edited by hand. sed's
// range would run to the end of it, so it is refused and left alone.
func TestSyncingRefusesABlockWithNoEnd(t *testing.T) {
	host := newShellHost(t, "active")
	broken := operatorRules + "\n" + dockerBeginMarker + "\n# half a block\n# the operator's last line\n"
	svc, rules := syncFixture(t, host, fakeExposed{ports: []int{15002}}, broken)

	if err := svc.SyncPublished(context.Background()); err == nil {
		t.Fatal("a block with no end marker was replaced")
	}
	if got, _ := os.ReadFile(rules); string(got) != broken {
		t.Errorf("the file changed:\n%s", got)
	}
	if calls := host.ufwCalls(t); calls != "" {
		t.Errorf("ufw was asked for %q", calls)
	}
}

// Not knowing what is exposed is an error, and nothing reaches the host.
func TestSyncingReturnsWhatExposedCouldNotAnswer(t *testing.T) {
	host := &fakeHost{}
	failure := errors.New("database gone")
	svc := NewService(host, nil, fakeExposed{err: failure}, t.TempDir())

	if err := svc.SyncPublished(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("sync: %v", err)
	}
	if len(host.ran) != 0 {
		t.Errorf("the host was asked anyway: %v", host.ran)
	}
}

// The file travels through the data directory, and the script moves a
// temporary copy over after.rules rather than editing it in place.
func TestSyncingHandsTheBlockOverAsAFile(t *testing.T) {
	host := &fakeHost{}
	dataDir := t.TempDir()
	svc := NewService(host, nil, fakeExposed{ports: []int{15002}}, dataDir)

	if err := svc.SyncPublished(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(host.ran) != 1 {
		t.Fatalf("ran %v", host.ran)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "ufw-docker.rules"))
	if err != nil || !strings.Contains(string(got), "--ctorigdstport 15002 ") {
		t.Errorf("the block in the data directory: %q %v", got, err)
	}
	script := replaceScript(filepath.Join(dataDir, "ufw-docker.rules"), afterRules)
	for _, want := range []string{"mktemp", `mv -f "$tmp" "$rules"`, "ufw reload"} {
		if !strings.Contains(script, want) {
			t.Errorf("the script has no %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "sed -i") {
		t.Errorf("the script edits after.rules in place:\n%s", script)
	}
}

// Adopting installs the stanza with what is already exposed in it, and
// writes no ufw rule for those ports: one would name the port inside the
// container, which every database of that engine listens on.
func TestAdoptingKeepsWhatIsExposedReachableByItsPublishedPort(t *testing.T) {
	host := &fakeHost{status: status(active, "", "", "0", "22")}
	dataDir := t.TempDir()
	svc := NewService(host, nil, fakeExposed{ports: []int{15002}}, dataDir)

	if _, err := svc.AdoptDocker(context.Background(), admin, []int{15002, 15000}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	all := strings.Join(host.ran, "\n")
	if strings.Contains(all, "port 15002") {
		t.Errorf("a ufw rule was written for an exposed port: %v", host.ran)
	}
	if !strings.Contains(all, "port 15000") {
		t.Errorf("a port that is not exposed lost its rule: %v", host.ran)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "ufw-docker.rules"))
	if err != nil || !strings.Contains(string(got), "--ctorigdstport 15002 ") {
		t.Errorf("the stanza adopting wrote: %q %v", got, err)
	}
}
