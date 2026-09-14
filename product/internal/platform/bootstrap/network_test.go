package bootstrap

import (
	"context"
	"errors"
	"slices"
	"testing"

	"cubeship/internal/platform/dockerx"
)

type topologyEngine struct {
	created []string
	added   map[string][]string
	removed map[string][]string
	fail    string
}

func (f *topologyEngine) EnsureNetwork(_ context.Context, name string) error {
	f.created = append(f.created, name)
	return nil
}
func (f *topologyEngine) ReconcileNetworks(_ context.Context, name string, add, remove []string) error {
	if name == f.fail {
		return errors.New("attachment failed")
	}
	f.added[name] = add
	f.removed[name] = remove
	return nil
}
func TestUpgradeSeparatesExistingControlPlaneBeforeServing(t *testing.T) {
	f := &topologyEngine{added: map[string][]string{}, removed: map[string][]string{}}
	if err := PrepareNetworks(context.Background(), f, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{DaemonContainerName, PostgresContainerName, RegistryContainerName, FrontendContainerName, BuildKitContainerName} {
		if !slices.Contains(f.added[name], dockerx.ManagementNetwork) || !slices.Contains(f.removed[name], Network) {
			t.Errorf("%s was not isolated: +%v -%v", name, f.added[name], f.removed[name])
		}
	}
	if len(f.removed[TraefikContainerName]) != 0 {
		t.Fatal("ingress lost the application bridge")
	}
	f.fail = DaemonContainerName
	if err := PrepareNetworks(context.Background(), f, true); err == nil {
		t.Fatal("migration failure must prevent startup")
	}
}

func TestDaemonReplacementClosesPublicRecoveryPorts(t *testing.T) {
	old := dockerx.ContainerOpts{Network: Network, AlsoNetworks: []string{"external-db"}, Ports: []string{"0.0.0.0:3000:3000/tcp", "[::]:3000:3000/tcp"}}
	next, err := DaemonReplacement(old)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(next.Ports, []string{"127.0.0.1:3000:3000/tcp"}) {
		t.Fatal(next.Ports)
	}
	if next.Network != dockerx.ManagementNetwork || !slices.Equal(next.AlsoNetworks, []string{"external-db"}) {
		t.Fatalf("%+v", next)
	}
	if old.Ports[0] != "0.0.0.0:3000:3000/tcp" {
		t.Fatal("mutated original options")
	}
}
