package datastore

import (
	"context"
	"io"
	"strings"
	"testing"

	"cubeship/internal/firewall"
	"cubeship/internal/metrics"
	"cubeship/internal/platform/database/dbtest"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
	"cubeship/internal/settings"
	"cubeship/internal/user"
)

// firewallFakeHost is a firewall.Host that records every command it is
// asked to run, so a test can assert on argv without a real ufw
// anywhere.
type firewallFakeHost struct {
	available bool
	ran       []string
}

func (f *firewallFakeHost) Available() bool { return f.available }

func (f *firewallFakeHost) Run(_ context.Context, argv ...string) (hostexec.Result, error) {
	f.ran = append(f.ran, strings.Join(argv, " "))
	return hostexec.Result{}, nil
}

func (f *firewallFakeHost) Script(context.Context, string) (hostexec.Result, error) {
	return hostexec.Result{}, nil
}

// firewallFakeDocker is enough of DockerAPI to let a datastore
// provision without a real Docker: creating and starting always
// succeed, and the container reports itself running, so waitReady
// returns on its first check.
type firewallFakeDocker struct{}

func (firewallFakeDocker) PullImage(context.Context, string, *dockerx.RegistryAuth) error {
	return nil
}
func (firewallFakeDocker) CreateContainer(context.Context, dockerx.ContainerOpts) (string, error) {
	return "fake-container", nil
}
func (firewallFakeDocker) StartContainer(context.Context, string) error  { return nil }
func (firewallFakeDocker) StopContainer(context.Context, string) error   { return nil }
func (firewallFakeDocker) RemoveContainer(context.Context, string) error { return nil }
func (firewallFakeDocker) SetResources(context.Context, string, dockerx.Resources) error {
	return nil
}
func (firewallFakeDocker) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (firewallFakeDocker) Logs(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (firewallFakeDocker) ExecStream(context.Context, string, []string, io.Reader, io.Writer) (string, int, error) {
	return "", 0, nil
}

var firewallTestAdmin = &user.User{Username: "admin", Role: user.RoleAdmin}

// newFirewallFixture is a Service over a real, throwaway database, with
// no app module wired in: Expose, Unexpose and Delete never reach it,
// and every test here is about the firewall rule those go through
// instead.
func newFirewallFixture(t *testing.T, host firewall.Host) *Service {
	t.Helper()
	db := dbtest.New(t)
	prov := NewProvisioner(db, firewallFakeDocker{}, t.TempDir())
	// The fake container reports running on the very first check, so
	// there is nothing here for a real interval to wait out.
	prov.ReadyAttempts = 1
	svc := NewService(db, nil, prov, settings.NewService(db), metrics.NewService(db), host)
	t.Cleanup(svc.WaitForProvisioning)
	return svc
}

func createForExposing(t *testing.T, svc *Service, slug string, engine Engine) *Datastore {
	t.Helper()
	d, err := svc.Create(context.Background(), firewallTestAdmin, Spec{Slug: slug, Engine: engine})
	if err != nil {
		t.Fatalf("create %s: %v", slug, err)
	}
	svc.WaitForProvisioning()
	return d
}

// The bug this whole change fixes: a forwarded rule is consulted after
// Docker has already translated the packet, so a rule naming the
// published port — 15000-something — never matches what arrives
// addressed to 5432. Exposing has to write the engine's own port, never
// the one handed back to whoever asked.
func TestExposeAdmitsTheInsidePortNotThePublishedOne(t *testing.T) {
	host := &firewallFakeHost{available: true}
	svc := newFirewallFixture(t, host)
	d := createForExposing(t, svc, "pg", EnginePostgres)

	if _, err := svc.Expose(context.Background(), firewallTestAdmin, d.Slug, 0); err != nil {
		t.Fatalf("expose: %v", err)
	}
	svc.WaitForProvisioning()

	want := "ufw route allow proto tcp from any to any port 5432 comment cubeship"
	var wrote bool
	for _, cmd := range host.ran {
		// Every port this range hands out starts with "15", so a rule
		// naming the published one instead of the engine's own would
		// show up as exactly this substring.
		if strings.Contains(cmd, "port 15") {
			t.Errorf("a rule named the published port instead of the inside one: %q", cmd)
		}
		if cmd == want {
			wrote = true
		}
	}
	if !wrote {
		t.Errorf("no rule was written for the inside port: %v", host.ran)
	}
}

// Create never goes through setPort, so a datastore exposed the moment
// it is made — Spec.Expose, not a later call to Expose — needs its own
// admit or it would sit unreachable until the daemon's next start swept
// it up.
func TestCreatingAnAlreadyExposedDatastoreAdmitsItsPort(t *testing.T) {
	host := &firewallFakeHost{available: true}
	svc := newFirewallFixture(t, host)
	port := PortRangeStart
	_, err := svc.Create(context.Background(), firewallTestAdmin, Spec{
		Slug: "pg", Engine: EnginePostgres, Expose: &port,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	svc.WaitForProvisioning()

	want := "ufw route allow proto tcp from any to any port 5432 comment cubeship"
	for _, cmd := range host.ran {
		if cmd == want {
			return
		}
	}
	t.Errorf("no rule was admitted for a datastore exposed at creation: %v", host.ran)
}

// The rule is written for the inside port, not for one row — MySQL and
// MariaDB both listen on 3306 — so unexposing one of two must not close
// the rule the other still needs.
func TestUnexposingOneOfTwoSharingAPortDoesNotRemoveTheRule(t *testing.T) {
	host := &firewallFakeHost{available: true}
	svc := newFirewallFixture(t, host)
	mysql := createForExposing(t, svc, "mysql", EngineMySQL)
	mariadb := createForExposing(t, svc, "mariadb", EngineMariaDB)

	if _, err := svc.Expose(context.Background(), firewallTestAdmin, mysql.Slug, 0); err != nil {
		t.Fatalf("expose mysql: %v", err)
	}
	if _, err := svc.Expose(context.Background(), firewallTestAdmin, mariadb.Slug, 0); err != nil {
		t.Fatalf("expose mariadb: %v", err)
	}
	svc.WaitForProvisioning()

	if _, err := svc.Unexpose(context.Background(), firewallTestAdmin, mysql.Slug); err != nil {
		t.Fatalf("unexpose mysql: %v", err)
	}
	svc.WaitForProvisioning()

	for _, cmd := range host.ran {
		if strings.Contains(cmd, "delete") && strings.Contains(cmd, "port 3306") {
			t.Fatalf("the shared rule was removed while mariadb still needs it: %v", host.ran)
		}
	}
}

// And the mirror of the test above: once nothing is left on the port,
// the rule goes.
func TestUnexposingTheLastOneOnAPortRemovesTheRule(t *testing.T) {
	host := &firewallFakeHost{available: true}
	svc := newFirewallFixture(t, host)
	d := createForExposing(t, svc, "pg", EnginePostgres)

	if _, err := svc.Expose(context.Background(), firewallTestAdmin, d.Slug, 0); err != nil {
		t.Fatalf("expose: %v", err)
	}
	svc.WaitForProvisioning()

	if _, err := svc.Unexpose(context.Background(), firewallTestAdmin, d.Slug); err != nil {
		t.Fatalf("unexpose: %v", err)
	}
	svc.WaitForProvisioning()

	want := "ufw route delete allow proto tcp from any to any port 5432 comment cubeship"
	for _, cmd := range host.ran {
		if cmd == want {
			return
		}
	}
	t.Errorf("the rule was not withdrawn: %v", host.ran)
}

// A host with no ufw, or none at all, is not a reason to refuse the
// expose that got here — the same guarantee mesh.Admit keeps.
func TestANilHostDoesNotFailAnExpose(t *testing.T) {
	svc := newFirewallFixture(t, nil)
	d := createForExposing(t, svc, "pg", EnginePostgres)

	if _, err := svc.Expose(context.Background(), firewallTestAdmin, d.Slug, 0); err != nil {
		t.Fatalf("expose with no host: %v", err)
	}
	svc.WaitForProvisioning()
}

// AdmitExposed is what repairs an instance exposed before this existed:
// every already-exposed datastore gets its rule the next time the
// daemon starts.
func TestAdmitExposedWritesTheRuleForWhatIsAlreadyExposed(t *testing.T) {
	host := &firewallFakeHost{available: true}
	svc := newFirewallFixture(t, host)
	d := createForExposing(t, svc, "pg", EnginePostgres)
	if _, err := svc.Expose(context.Background(), firewallTestAdmin, d.Slug, 0); err != nil {
		t.Fatalf("expose: %v", err)
	}
	svc.WaitForProvisioning()

	// Clear what exposing already wrote, so this only sees what the
	// sweep itself does.
	host.ran = nil
	svc.AdmitExposed(context.Background())

	want := "ufw route allow proto tcp from any to any port 5432 comment cubeship"
	for _, cmd := range host.ran {
		if cmd == want {
			return
		}
	}
	t.Errorf("the sweep did not admit an exposed datastore: %v", host.ran)
}

// A daemon with no way onto the host does not stop to list every
// datastore first — there would be nothing to do with the answer.
func TestAdmitExposedWithNoHostDoesNothing(t *testing.T) {
	svc := newFirewallFixture(t, nil)
	svc.AdmitExposed(context.Background())
}
