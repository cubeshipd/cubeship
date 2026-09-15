package templateinstall

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// A template that asks for extensions, and the release after it that
// asks for different ones.
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

const vectorTemplateNext = `version: 1
minCubeship: "0.9.0"
project: vectors
databases:
  - key: db
    name: vectors-db
    engine: postgres
    version: "16"
    extensions:
      - pgvector
      - vectorchord
      - pg_trgm
apps:
  - key: web
    name: web
    image: ghcr.io/example/web
    tag: "1.1.0"
    port: 3000
    attach:
      - database: db
`

func vectorFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture("0.9.0")
	f.catalog.sources["abc1234"] = vectorTemplate
	f.catalog.sources["def5678"] = vectorTemplateNext
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

// A newer release asking for different extensions does **not** change a
// database that already exists — the same answer an engine or a version
// change gets, and for a related reason: the extensions chose the image.
//
// It is said in the preview rather than silently dropped, because the
// fix is one an operator can carry out on the database itself.
func TestAnUpdateThatChangesExtensionsLeavesTheDatabaseAlone(t *testing.T) {
	f := vectorFixture(t)
	in, _, _ := f.install(t, vectorRequest())
	f.catalog.publish()

	before := len(f.w.events())
	preview, err := f.s.PreviewUpdate(context.Background(), admin, in.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.w.events()) != before {
		t.Errorf("a preview did %v", f.w.events()[before:])
	}

	var said *Change
	for i, c := range preview.Changes {
		if c.Kind == KindDatabase && c.Name == "vectors-db" {
			said = &preview.Changes[i]
		}
	}
	if said == nil {
		t.Fatalf("the preview says nothing about the database: %+v", preview.Changes)
	}
	if said.Action != ActionKeep {
		t.Errorf("the preview would %s the database: %+v", said.Action, said)
	}
	for _, want := range []string{"pg_trgm", "install them on the database itself"} {
		if !strings.Contains(said.Detail, want) {
			t.Errorf("the preview does not say %q: %q", want, said.Detail)
		}
	}

	// And the update itself leaves it exactly as it was.
	if _, _, err := f.s.Update(context.Background(), admin, in.ID, UpdateRequest{}); err != nil {
		t.Fatal(err)
	}
	f.s.Wait()
	if n := len(f.w.specs); n != 1 {
		t.Fatalf("the update created %d databases, want none", n-1)
	}
	if !slices.Equal(f.w.specs[0].Extensions, []string{"pgvector", "vectorchord"}) {
		t.Errorf("the database now has %v", f.w.specs[0].Extensions)
	}
}
