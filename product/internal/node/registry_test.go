package node

import (
	"context"
	"errors"
	"testing"
)

type registryPlacements struct {
	Apps
	placements map[int64][]Placement
	err        error
}

func (a registryPlacements) PlacementsFor(_ context.Context, id int64) ([]Placement, error) {
	return a.placements[id], a.err
}

func TestNodePullUsesOnlyItsDesiredRepositories(t *testing.T) {
	ctx := context.Background()
	desired := registryPlacements{placements: map[int64][]Placement{
		7: {{Image: "registry.example:443/team/production/api:v2"}, {Image: "external.example/other/production/app:v1"}},
		8: {{Image: "registry.example:443/other/production/app:v1"}},
	}}
	s := &Service{apps: desired, registry: func(context.Context) string { return "registry.example:443" }}
	for _, tc := range []struct {
		id   int64
		repo string
		want bool
	}{
		{7, "team/production/api", true},
		{7, "other/production/app", false},
		{8, "other/production/app", true},
		{9, "team/production/api", false},
		{7, "team/production/api-extra", false},
		{7, "team/production/api:v2", false},
		{7, "", false},
	} {
		got, err := s.NodeCanPull(ctx, tc.id, tc.repo)
		if err != nil || got != tc.want {
			t.Errorf("node %d repo %q: got %v, %v", tc.id, tc.repo, got, err)
		}
	}
	// A failed deployment changes the desired image back to the retained one.
	desired.placements[7] = []Placement{{Image: "registry.example:443/team/production/api:v1"}}
	if got, err := s.NodeCanPull(ctx, 7, "team/production/api"); !got || err != nil {
		t.Fatalf("rollback denied: %v %v", got, err)
	}
	delete(desired.placements, 7)
	if got, _ := s.NodeCanPull(ctx, 7, "team/production/api"); got {
		t.Fatal("removed placement still granted")
	}
	s.apps = registryPlacements{err: errors.New("lookup failed"), placements: map[int64][]Placement{7: {{Image: "registry.example:443/team/production/api:v1"}}}}
	if got, err := s.NodeCanPull(ctx, 7, "team/production/api"); got || err == nil {
		t.Fatalf("lookup failure did not fail closed: %v %v", got, err)
	}
}

func TestRegistryRepositoryMatchesAuthorityAndImageName(t *testing.T) {
	for _, tc := range []struct{ image, want string }{
		{"registry.example:443/team/app:v2", "team/app"},
		{"registry.example:443/team/app@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "team/app"},
		{"registry.example:443/team/app:v2@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "team/app"},
		{"registry.example:443.evil/team/app:v2", ""},
		{"external.example/registry.example:443/team/app:v2", ""},
		{"registry.example/team/app:v2", ""},
		{"registry.example:443/../team/app:v2", ""},
		{"registry.example:443/team/app@invalid", ""},
	} {
		if got := registryRepository(tc.image, "registry.example:443"); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.image, got, tc.want)
		}
	}
}
