package server

import (
	"context"
	"slices"
	"testing"

	"cubeship/internal/datastore"
	"cubeship/internal/objectstore"
	"cubeship/internal/platform/database/dbtest"
)

// What the firewall is told is exposed: a datastore's published port and
// a managed store's, and nothing for a datastore that is not exposed or
// for a linked store, which has no container to publish anything.
func TestExposedPortsAreEveryDatastoreAndManagedStore(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	datastores := datastore.NewRepository(db)
	stores := objectstore.NewRepository(db)

	for _, d := range []*datastore.Datastore{
		{Slug: "pg", Engine: datastore.EnginePostgres, Version: "17", Username: "u", Password: "p", ExposedPort: 15002},
		{Slug: "quiet", Engine: datastore.EnginePostgres, Version: "17", Username: "u", Password: "p"},
	} {
		if _, err := datastores.Create(ctx, d); err != nil {
			t.Fatalf("create datastore %s: %v", d.Slug, err)
		}
	}
	for _, s := range []*objectstore.Store{
		{Slug: "media", Kind: objectstore.KindManaged, Provider: objectstore.ProviderMinIO, ExposedPort: 16000},
		// A number no linked store should have, so that counting it
		// would show.
		{Slug: "ext", Kind: objectstore.KindExternal, Provider: objectstore.ProviderGeneric,
			Endpoint: "s3.example.com", ExposedPort: 16001},
	} {
		if _, err := stores.Create(ctx, s); err != nil {
			t.Fatalf("create store %s: %v", s.Slug, err)
		}
	}

	got, err := exposedPorts{datastores, stores}.ExposedPorts(ctx)
	if err != nil {
		t.Fatalf("exposed ports: %v", err)
	}
	slices.Sort(got)
	if !slices.Equal(got, []int{15002, 16000}) {
		t.Errorf("exposed ports: %v", got)
	}
}
