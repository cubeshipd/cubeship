package mesh

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cubeship/internal/firewall"
	"cubeship/internal/platform/dockerx"
	"cubeship/internal/platform/hostexec"
)

type fakeEngine struct {
	state     dockerx.SwarmState
	initAt    string
	joinedTo  string
	joinedAs  string
	withToken string
	networks  []string
	token     string
}

func (f *fakeEngine) Swarm(context.Context) (dockerx.SwarmState, error) { return f.state, nil }

func (f *fakeEngine) SwarmInit(_ context.Context, advertise string) error {
	f.initAt = advertise
	f.state = dockerx.SwarmState{Active: true, Manager: true, NodeID: "manager-1"}
	return nil
}

func (f *fakeEngine) SwarmWorkerToken(context.Context) (string, error) { return f.token, nil }

func (f *fakeEngine) SwarmJoin(_ context.Context, manager, token, advertise string) error {
	f.joinedTo, f.withToken, f.joinedAs = manager, token, advertise
	f.state = dockerx.SwarmState{Active: true, NodeID: "worker-1"}
	return nil
}

func (f *fakeEngine) EnsureOverlayNetwork(_ context.Context, name string) error {
	f.networks = append(f.networks, name)
	return nil
}

// The control plane is the manager, and the address it advertises is
// where every other machine will try to reach it. It is the one field
// in a swarm that cannot be corrected later without tearing the cluster
// down, which is why an empty one is refused rather than passed on.
func TestTheClusterIsBuiltAtAnAddressTheOthersCanReach(t *testing.T) {
	engine := &fakeEngine{token: "SWMTKN-1-worker"}

	info, err := Ensure(context.Background(), engine, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if engine.initAt != "203.0.113.9" {
		t.Errorf("the swarm advertises %q", engine.initAt)
	}
	if info.Manager != "203.0.113.9:2377" {
		t.Errorf("a worker would join at %q", info.Manager)
	}
	if info.JoinToken != "SWMTKN-1-worker" {
		t.Errorf("the join token is %q, and it is Docker's to mint", info.JoinToken)
	}
	if len(engine.networks) != 1 || engine.networks[0] != NetworkName {
		t.Errorf("networks created: %v", engine.networks)
	}

	if _, err := Ensure(context.Background(), &fakeEngine{}, ""); err == nil {
		t.Error("a cluster was built at no address at all")
	}
}

// Called again, it changes nothing: a swarm that exists is left alone.
// Adding a second machine to a cluster must not be an act that touches
// the first one's Docker.
func TestBuildingTheClusterTwiceLeavesTheFirstAlone(t *testing.T) {
	engine := &fakeEngine{
		state: dockerx.SwarmState{Active: true, Manager: true, NodeID: "manager-1"},
		token: "SWMTKN-1-worker",
	}
	if _, err := Ensure(context.Background(), engine, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if engine.initAt != "" {
		t.Error("a swarm that was already running was initialised again")
	}
}

// A Docker that is already a worker in somebody else's swarm is left
// exactly as it is. Cubeship did not put it there, and the fix —
// `docker swarm leave` — is one that takes down whatever else is using
// it, so it belongs to the person who set that up.
func TestAnEngineInSomebodyElsesSwarmIsNotTakenOver(t *testing.T) {
	engine := &fakeEngine{state: dockerx.SwarmState{Active: true, Manager: false}}
	_, err := Ensure(context.Background(), engine, "203.0.113.9")
	if err == nil {
		t.Fatal("a machine was torn out of a swarm it already belonged to")
	}
	if !strings.Contains(err.Error(), "swarm leave") {
		t.Errorf("the refusal is %q, and it has to say what to do about it", err)
	}
}

// A worker joins once. The second pass — and every pass after it, ten
// seconds apart forever — must not re-join a machine that is already in
// the cluster.
func TestAWorkerJoinsOnce(t *testing.T) {
	engine := &fakeEngine{}
	info := Info{Manager: "203.0.113.9:2377", JoinToken: "SWMTKN-1-worker"}

	if err := Join(context.Background(), engine, info, "198.51.100.4"); err != nil {
		t.Fatal(err)
	}
	if engine.joinedTo != "203.0.113.9:2377" || engine.withToken != "SWMTKN-1-worker" {
		t.Errorf("joined %q with %q", engine.joinedTo, engine.withToken)
	}
	if engine.joinedAs != "198.51.100.4" {
		t.Errorf("the machine advertised itself as %q", engine.joinedAs)
	}
	// A worker does not create the overlay: it belongs to the manager,
	// and it reaches a machine when a container there attaches to it.
	if len(engine.networks) != 0 {
		t.Errorf("a worker created %v", engine.networks)
	}

	engine.joinedTo = ""
	if err := Join(context.Background(), engine, info, "198.51.100.4"); err != nil {
		t.Fatal(err)
	}
	if engine.joinedTo != "" {
		t.Error("a machine already in the cluster joined it again")
	}
}

type fakeHost struct {
	available bool
	ran       [][]string
	err       error
}

func (f *fakeHost) Available() bool { return f.available }

func (f *fakeHost) Run(_ context.Context, argv ...string) (hostexec.Result, error) {
	f.ran = append(f.ran, argv)
	return hostexec.Result{Code: 0}, f.err
}

func (f *fakeHost) Script(context.Context, string) (hostexec.Result, error) {
	return hostexec.Result{Code: 0}, nil
}

// What is opened is opened **to the peers**, never to anywhere. That is
// the whole reason these ports are acceptable at all: the control plane
// knows every machine's address, because every agent reports one, so a
// cluster port is open to the cluster rather than to the internet.
func TestTheClustersPortsAreOpenedToThePeersAndNobodyElse(t *testing.T) {
	host := &fakeHost{available: true}
	if err := Admit(context.Background(), host, []string{"198.51.100.4"}, true); err != nil {
		t.Fatal(err)
	}
	if len(host.ran) != len(Ports) {
		t.Fatalf("ran %d commands for %d ports", len(host.ran), len(Ports))
	}
	for _, argv := range host.ran {
		line := strings.Join(argv, " ")
		if !strings.Contains(line, "from 198.51.100.4") {
			t.Errorf("%q is not scoped to the peer", line)
		}
		if strings.Contains(line, "from any") {
			t.Errorf("%q opens a cluster port to anywhere", line)
		}
		// The host's own traffic, not a container's: `ufw route` is for
		// a published port and would need the DOCKER-USER adoption
		// these rules deliberately do not.
		if strings.Contains(line, "route") {
			t.Errorf("%q is a forwarding rule; the swarm's ports are the host's own", line)
		}
	}
}

// Only the manager listens for joins. Opening 2377 on a worker is not
// dangerous, and it is a rule that says something untrue about the
// machine — which is the kind of thing somebody reads off a firewall
// screen and reasons from.
func TestOnlyTheManagerAdmitsJoins(t *testing.T) {
	worker := &fakeHost{available: true}
	if err := Admit(context.Background(), worker, []string{"203.0.113.9"}, false); err != nil {
		t.Fatal(err)
	}
	for _, argv := range worker.ran {
		if strings.Contains(strings.Join(argv, " "), "port 2377") {
			t.Error("a worker opened the port only a manager listens on")
		}
	}
	if len(worker.ran) != len(Ports)-1 {
		t.Errorf("a worker wrote %d rules, want one fewer than the manager's %d", len(worker.ran), len(Ports))
	}
}

// A host with no ufw is a host with nothing blocking these ports, and a
// firewall this daemon cannot reach is one somebody else is managing.
// Neither is a reason to refuse to cluster.
func TestAMachineWithNoFirewallStillClusters(t *testing.T) {
	if err := Admit(context.Background(), nil, []string{"203.0.113.9"}, true); err != nil {
		t.Errorf("no host at all: %v", err)
	}
	if err := Admit(context.Background(), &fakeHost{available: false}, []string{"203.0.113.9"}, true); err != nil {
		t.Errorf("an unreachable host: %v", err)
	}
}

// A peer that is not an address is a machine that has not reported one.
// Nothing user-supplied reaches a command line here — firewall.Spec is
// what guarantees that — so the rule is skipped rather than guessed at.
func TestAPeerThatIsNotAnAddressIsSkipped(t *testing.T) {
	host := &fakeHost{available: true}
	if err := Admit(context.Background(), host, []string{"not-an-address; rm -rf /"}, true); err != nil {
		t.Fatal(err)
	}
	if len(host.ran) != 0 {
		t.Errorf("ran %v for something that is not an address", host.ran)
	}
}

// A firewall that refuses is reported, not swallowed: the cluster will
// not form, and the reason is worth having in a log.
func TestAFirewallThatRefusesIsReported(t *testing.T) {
	host := &fakeHost{available: true, err: errors.New("ufw is not installed")}
	if err := Admit(context.Background(), host, []string{"203.0.113.9"}, true); err == nil {
		t.Error("a firewall that could not be written to was reported as fine")
	}
}

// The ports are the host's own, and every rule says which machine it is
// for. A firewall screen that shows four rules with no explanation is
// four rules somebody deletes.
func TestEveryRuleSaysWhatItIsFor(t *testing.T) {
	for _, port := range Ports {
		if port.Why == "" {
			t.Errorf("port %d carries no comment", port.Number)
		}
		spec := firewall.Spec{
			Scope: firewall.ScopeHost, Action: firewall.ActionAllow,
			Protocol: port.Protocol, Port: "1234", From: "203.0.113.9", Comment: port.Why,
		}
		if err := spec.Check(); err != nil {
			t.Errorf("port %d builds a rule the firewall refuses: %v", port.Number, err)
		}
	}
}
