package objectstore

import (
	"context"
	"slices"
	"testing"

	"cubeship/internal/platform/dockerx"
)

func TestManagedStoreReachableFromAppsAndControlPlane(t *testing.T) {
	p := NewProvisioner(nil, nil, "")
	p.SetMeshNetwork(func(context.Context) string { return "mesh" })
	opts := p.containerOpts(context.Background(), &Store{Slug: "files", Version: DefaultVersion()})
	if opts.Network != dockerx.ApplicationNetwork || !slices.Contains(opts.AlsoNetworks, dockerx.ManagementNetwork) || !slices.Contains(opts.AlsoNetworks, "mesh") {
		t.Fatalf("managed store loses a required client network: %+v", opts)
	}
	if len(opts.Ports) != 0 {
		t.Fatal("private store published a host port")
	}
}
