package app

import (
	"slices"
	"testing"
)

func TestWhichTCPPortsArePublished(t *testing.T) {
	for _, tc := range []struct {
		container, host int
		want            bool
	}{
		{22, 0, true}, {22, 2222, true}, {65535, 65535, true}, {22, 1024, true},
		{0, 2222, false}, {65536, 0, false}, {22, 22, false}, {22, 1023, false}, {22, 65536, false},
	} {
		if got := validTCPPorts(tc.container, tc.host); got != tc.want {
			t.Errorf("%d on %d: got %v, want %v", tc.container, tc.host, got, tc.want)
		}
	}
}

// A picked port skips whatever anything on the instance holds, and says so
// when the range is full.
func TestAPickedTCPPortSkipsTheTakenOnes(t *testing.T) {
	port, err := pickTCPPort(map[int]bool{TCPPortRangeStart: true, TCPPortRangeStart + 1: true})
	if err != nil || port != TCPPortRangeStart+2 {
		t.Fatalf("picked %d, %v", port, err)
	}
	full := map[int]bool{}
	for p := TCPPortRangeStart; p <= TCPPortRangeEnd; p++ {
		full[p] = true
	}
	if _, err := pickTCPPort(full); err != ErrNoTCPPortsLeft {
		t.Fatalf("a full range gave %v", err)
	}
}

// Only one copy on the control plane may publish, and nothing may move or
// multiply it afterwards.
func TestAPublishedPortPinsTheAppToTheControlPlane(t *testing.T) {
	const here = 1
	one := func(node int64) App { return App{Replicas: []Replica{{NodeID: node, NodeSlug: "control-plane"}}} }

	if a := one(here); !canPublishTCP(&a, here) {
		t.Error("one copy on the control plane cannot publish")
	}
	if a := one(2); canPublishTCP(&a, here) {
		t.Error("an app on a worker can publish")
	}
	two := App{Replicas: []Replica{{NodeID: here}, {NodeID: here, Ordinal: 2}}}
	if canPublishTCP(&two, here) {
		t.Error("two copies can publish")
	}
	spread := one(here)
	spread.Spread = true
	if canPublishTCP(&spread, here) {
		t.Error("a spread app can publish")
	}

	pinned := &Scoped{App: one(here)}
	yes := true
	for _, tc := range []struct {
		name string
		p    Placement
		want bool
	}{
		{"nothing about placement", Placement{}, true},
		{"the same machine", Placement{Nodes: []string{"control-plane"}}, true},
		{"another machine", Placement{Nodes: []string{"worker-1"}}, false},
		{"two copies", Placement{Replicas: 2}, false},
		{"spread", Placement{Spread: &yes}, false},
	} {
		if got := keepsTCP(pinned, tc.p); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTCPPortsAreBoundAsHostToContainer(t *testing.T) {
	got := tcpPortSpecs([]TCPPort{{ContainerPort: 22, HostPort: 2222}, {ContainerPort: 1883, HostPort: 17000}})
	if want := []string{"2222:22/tcp", "17000:1883/tcp"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if tcpPortSpecs(nil) != nil {
		t.Fatal("no ports bound something")
	}
}
