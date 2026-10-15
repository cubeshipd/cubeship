package templateinstall

import (
	"slices"
	"testing"
)

// A template that asks for extensions.
const vectorTemplate = `version: 1
minCubeship: "0.9.0"
project: vectors
databases:
  - key: db
    name: vectors-db
    engine: postgres
    version: "16"
    extensions:
      - vectorchord
apps:
  - key: web
    name: web
    image: ghcr.io/example/web
    tag: "1.0.0"
    port: 3000
    attach:
      - database: db
`

func vectorFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture("0.9.0")
	f.catalog.sources["abc1234"] = vectorTemplate
	return f
}

func vectorRequest() Request {
	return Request{Owner: "cubeshipd", Repo: "cubeship-vectors-template"}
}

// The install hands the daemon the normalized list — and asking for
// VectorChord alone is asking for pgvector too, which is the whole
// reason a template does not have to spell it out.
func TestAnInstallCreatesTheDatabaseWithItsExtensions(t *testing.T) {
	f := vectorFixture(t)
	in, run, _ := f.install(t, vectorRequest())
	if in.Status != StatusInstalled || run.Status != RunSucceeded {
		t.Fatalf("installation = %+v, run = %+v", in, run)
	}
	spec := f.w.specs[0]
	if !slices.Equal(spec.Extensions, []string{"pgvector", "vectorchord"}) {
		t.Fatalf("database created with %v", spec.Extensions)
	}
}
